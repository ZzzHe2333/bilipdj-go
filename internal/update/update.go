package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Asset struct {
	Name, URL string
	Size      int64
}
type Release struct {
	Version         string `json:"version"`
	Current         string `json:"current"`
	UpdateAvailable bool   `json:"update_available"`
	Asset           string `json:"asset"`
	URL             string `json:"url"`
	Notes           string `json:"notes"`
	HasSHA256       bool   `json:"has_sha256"`
}
type Downloaded struct {
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Message string `json:"message"`
}
type Service struct {
	Repo, Version, Dir string
	APIBase            string
	Client             *http.Client
}

func New(repo, version, dir string) *Service {
	return &Service{Repo: repo, Version: version, Dir: dir, Client: &http.Client{Timeout: 60 * time.Second}}
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Body   string    `json:"body"`
	Assets []ghAsset `json:"assets"`
}

func (s *Service) latestURL() string {
	if s.APIBase != "" {
		return strings.TrimSuffix(s.APIBase, "/") + "/releases/latest"
	}
	return "https://api.github.com/repos/" + s.Repo + "/releases/latest"
}
func (s *Service) latest(ctx context.Context) (ghRelease, ghAsset, ghAsset, error) {
	var release ghRelease
	req, _ := http.NewRequestWithContext(ctx, "GET", s.latestURL(), nil)
	req.Header.Set("User-Agent", "bilipdj-go-updater/0.1")
	resp, e := s.Client.Do(req)
	if e != nil {
		return release, ghAsset{}, ghAsset{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return release, ghAsset{}, ghAsset{}, fmt.Errorf("GitHub Release HTTP %d (仓库可能尚未发布版本)", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); e != nil {
		return release, ghAsset{}, ghAsset{}, e
	}
	want := fmt.Sprintf("bilipdj-go-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
	var asset, checksum ghAsset
	for _, a := range release.Assets {
		if a.Name == want {
			asset = a
		}
		if a.Name == want+".sha256" {
			checksum = a
		}
	}
	return release, asset, checksum, nil
}
func (s *Service) Check(ctx context.Context) (Release, error) {
	v, asset, sum, e := s.latest(ctx)
	if e != nil {
		return Release{}, e
	}
	hasAsset := asset.Name != "" && sum.Name != ""
	return Release{Version: v.Tag, Current: s.Version, UpdateAvailable: strings.TrimPrefix(v.Tag, "v") != strings.TrimPrefix(s.Version, "v") && hasAsset, Asset: asset.Name, URL: asset.URL, Notes: v.Body, HasSHA256: sum.Name != ""}, nil
}
func (s *Service) fetch(ctx context.Context, u string, max int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "bilipdj-go-updater/0.1")
	r, e := s.Client.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("download HTTP %d", r.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > max {
		return nil, errors.New("下载超出体积上限")
	}
	return b, nil
}
func (s *Service) Download(ctx context.Context) (Downloaded, error) {
	version, asset, checksum, e := s.latest(ctx)
	if e != nil {
		return Downloaded{}, e
	}
	if asset.Name == "" || checksum.Name == "" {
		return Downloaded{}, errors.New("Release 缺少当前系统 ZIP 或 sha256 校验文件，拒绝更新")
	}
	if asset.Size <= 0 || asset.Size > 250<<20 {
		return Downloaded{}, errors.New("Release ZIP 体积异常")
	}
	rawHash, e := s.fetch(ctx, checksum.URL, 1<<16)
	if e != nil {
		return Downloaded{}, e
	}
	fields := strings.Fields(string(rawHash))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return Downloaded{}, errors.New("无效的 SHA256 文件")
	}
	want, e := hex.DecodeString(fields[0])
	if e != nil || len(want) != sha256.Size {
		return Downloaded{}, errors.New("校验值无效")
	}
	if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") != asset.Name {
		return Downloaded{}, errors.New("校验文件名不匹配")
	}
	// Stream directly to a temporary file; a failed hash never replaces an existing stage.
	dir := filepath.Join(s.Dir, "updates")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return Downloaded{}, e
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	req.Header.Set("User-Agent", "bilipdj-go-updater/0.1")
	resp, e := s.Client.Do(req)
	if e != nil {
		return Downloaded{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Downloaded{}, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	tmp, e := os.CreateTemp(dir, ".update-*")
	if e != nil {
		return Downloaded{}, e
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, 250<<20+1))
	if e != nil {
		tmp.Close()
		return Downloaded{}, e
	}
	if n != asset.Size {
		tmp.Close()
		return Downloaded{}, errors.New("下载大小与 Release 不一致")
	}
	if !equal(h.Sum(nil), want) {
		tmp.Close()
		return Downloaded{}, errors.New("SHA256 不匹配，已拒绝安装")
	}
	if e = tmp.Close(); e != nil {
		return Downloaded{}, e
	}
	name := fmt.Sprintf("%s-%s", strings.TrimPrefix(version.Tag, "v"), asset.Name)
	target := filepath.Join(dir, name)
	if e = os.Rename(tmp.Name(), target); e != nil {
		return Downloaded{}, e
	}
	return Downloaded{File: target, SHA256: hex.EncodeToString(want), Message: "已校验并暂存更新包；解压替换程序并重启，Docker 请拉取新镜像"}, nil
}
func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
