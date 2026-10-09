package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type InstallJob struct {
	Target    string   `json:"target"`
	Staged    string   `json:"staged"`
	Backup    string   `json:"backup"`
	SHA256    string   `json:"sha256"`
	ZIP       string   `json:"zip"`
	ParentPID int      `json:"parent_pid"`
	Arguments []string `json:"arguments"`
	Dir       string   `json:"dir"`
	HealthURL string   `json:"health_url"`
	Version   string   `json:"version"`
}

// PrepareInstall validates the locally downloaded package again and extracts
// ONLY a regular executable, not arbitrary ZIP paths. The helper is a copy of
// the current executable so it can replace a locked Windows .exe after exit.
func PrepareInstall(d Downloaded, exe string, args []string, healthURL ...string) (string, string, error) {
	if !validHash(d.SHA256) {
		return "", "", errors.New("未验证的更新包")
	}
	cleanExe, err := filepath.Abs(exe)
	if err != nil {
		return "", "", err
	}
	zipName := fmt.Sprintf("bilipdj-go-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
	if !strings.HasSuffix(filepath.Base(d.File), "-"+zipName) {
		return "", "", errors.New("更新包与当前操作系统不匹配")
	}
	file, err := os.Open(d.File)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, io.LimitReader(file, maxPackage+1)); err != nil {
		return "", "", err
	}
	if !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), d.SHA256) {
		return "", "", errors.New("本地 ZIP 校验失败，拒绝安装")
	}
	zr, err := zip.OpenReader(d.File)
	if err != nil {
		return "", "", err
	}
	defer zr.Close()
	want := "bilipdj-go"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	var entry *zip.File
	for _, z := range zr.File {
		if z.Name == want {
			if entry != nil {
				return "", "", errors.New("更新包存在重复可执行文件")
			}
			entry = z
		}
	}
	if entry == nil || !entry.FileInfo().Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 90<<20 {
		return "", "", errors.New("更新 ZIP 中可执行文件缺失或异常")
	}
	updates := filepath.Dir(d.File)
	staged, err := os.CreateTemp(updates, ".staged-bin-*")
	if err != nil {
		return "", "", err
	}
	stagePath := staged.Name()
	defer func() {
		if err != nil {
			os.Remove(stagePath)
		}
	}()
	src, e := entry.Open()
	if e != nil {
		staged.Close()
		return "", "", e
	}
	n, e := io.Copy(staged, io.LimitReader(src, 90<<20+1))
	src.Close()
	if e == nil && uint64(n) != entry.UncompressedSize64 {
		e = errors.New("可执行文件长度不符")
	}
	if e == nil {
		e = staged.Chmod(0700)
	}
	if closeErr := staged.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return "", "", e
	}
	// Do not point a helper to a user-supplied executable. Only the running
	// process can call this with its own os.Executable() and derived location.
	helper := filepath.Join(updates, fmt.Sprintf("updater-%d%s", time.Now().UnixNano(), filepath.Ext(cleanExe)))
	if e = copyFile(cleanExe, helper, 0700); e != nil {
		return "", "", e
	}
	backup := cleanExe + ".backup-" + strings.TrimPrefix(d.Version, "v")
	if _, e = os.Stat(backup); e == nil {
		return "", "", errors.New("相同版本的旧程序备份已存在，请先手动检查")
	}
	job := InstallJob{Target: cleanExe, Staged: stagePath, Backup: backup, SHA256: d.SHA256, ZIP: d.File, ParentPID: os.Getpid(), Arguments: append([]string(nil), args...), Dir: filepath.Dir(cleanExe), Version: d.Version}
	if len(healthURL) > 0 {
		job.HealthURL = healthURL[0]
	}
	// One unique job file, permissions only current user.
	out, e := os.CreateTemp(updates, ".install-job-*.json")
	if e != nil {
		return "", "", e
	}
	jobPath := out.Name()
	encErr := json.NewEncoder(out).Encode(job)
	closeErr := out.Close()
	if encErr != nil {
		os.Remove(jobPath)
		return "", "", encErr
	}
	if closeErr != nil {
		os.Remove(jobPath)
		return "", "", closeErr
	}
	return helper, jobPath, nil
}
func copyFile(src, dst string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, io.LimitReader(in, 100<<20+1))
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(dst)
	}
	return e
}
func LaunchInstall(helper, job string) error {
	cmd := exec.Command(helper, "--apply-update", job)
	cmd.Dir = filepath.Dir(helper)
	setDetached(cmd)
	return cmd.Start()
}

// RunHelper is invoked as a separate executable copy of the old binary.
// It waits for the main process to exit, then switches the executable.
func RunHelper(jobPath string) error {
	b, e := os.ReadFile(jobPath)
	if e != nil {
		return e
	}
	var job InstallJob
	if e = json.Unmarshal(b, &job); e != nil {
		return e
	}
	if job.ParentPID < 1 || !filepath.IsAbs(job.Target) || !filepath.IsAbs(job.Staged) || !filepath.IsAbs(job.Backup) || !filepath.IsAbs(job.ZIP) {
		return errors.New("更新助手参数不合法")
	}
	if e = waitProcess(job.ParentPID, 45*time.Second); e != nil {
		return e
	}
	// Copy to the executable's directory first to avoid cross-volume renames.
	f, e := os.CreateTemp(filepath.Dir(job.Target), ".bilipdj-install-*")
	if e != nil {
		return e
	}
	temp := f.Name()
	f.Close()
	os.Remove(temp)
	defer os.Remove(temp)
	if e = copyFile(job.Staged, temp, 0755); e != nil {
		return fmt.Errorf("无法写入程序所在目录（请检查权限）：%w", e)
	}
	if e = os.Rename(job.Target, job.Backup); e != nil {
		return fmt.Errorf("旧程序备份失败，未执行替换：%w", e)
	}
	if e = os.Rename(temp, job.Target); e != nil {
		rollback := os.Rename(job.Backup, job.Target)
		return fmt.Errorf("新程序安装失败：%v，恢复旧版：%v", e, rollback)
	}
	cmd := exec.Command(job.Target, job.Arguments...)
	cmd.Dir = job.Dir
	setDetached(cmd)
	if e = cmd.Start(); e != nil {
		failed := job.Target + ".failed-" + fmt.Sprint(time.Now().Unix())
		_ = os.Rename(job.Target, failed)
		rollback := os.Rename(job.Backup, job.Target)
		if rollback == nil {
			old := exec.Command(job.Target, job.Arguments...)
			old.Dir = job.Dir
			setDetached(old)
			_ = old.Start()
		}
		return fmt.Errorf("新程序启动失败：%v，恢复旧版：%v", e, rollback)
	}
	if job.HealthURL != "" {
		client := &http.Client{Timeout: 800 * time.Millisecond}
		healthy := false
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			c, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
			request, _ := http.NewRequestWithContext(c, "GET", job.HealthURL, nil)
			resp, e := client.Do(request)
			if e == nil {
				var body struct {
					Version string `json:"version"`
				}
				decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
				resp.Body.Close()
				if resp.StatusCode == 200 && decodeErr == nil && "v"+body.Version == job.Version {
					healthy = true
					cancel()
					break
				}
			}
			cancel()
			time.Sleep(250 * time.Millisecond)
		}
		if !healthy {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			failed := job.Target + ".failed-" + fmt.Sprint(time.Now().Unix())
			_ = os.Rename(job.Target, failed)
			if err := os.Rename(job.Backup, job.Target); err != nil {
				return fmt.Errorf("新版未通过启动健康检查，而且回滚失败：%w", err)
			}
			old := exec.Command(job.Target, job.Arguments...)
			old.Dir = job.Dir
			setDetached(old)
			_ = old.Start()
			return errors.New("新版未通过启动健康检查，已恢复旧程序")
		}
	}
	_ = os.Remove(jobPath)
	_ = os.Remove(job.Staged)
	// Keep the old executable backup for manual rollback.
	return nil
}
