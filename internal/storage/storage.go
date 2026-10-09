// Package storage implements PR #312-compatible user data selection for Go.
// It intentionally never changes or removes Python's core/* records.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const choiceFile = ".storage-choice.json"

type Plan struct {
	Mode       string `json:"mode"`
	Legacy     string `json:"legacy"`
	User       string `json:"user"`
	Active     string `json:"active"`
	Choice     string `json:"choice"`
	Conflict   bool   `json:"conflict"`
	Migrate    bool   `json:"migrate"`
	OldHasData bool   `json:"old_has_data"`
	NewHasData bool   `json:"new_has_data"`
}

// Roots optionally carry a synthetic OS, home and environment to exercise the
// same policy on all 6 cross-compilation targets in CI.
func UserRoot(goos, home string, env map[string]string) string {
	if goos == "windows" {
		if env["APPDATA"] != "" {
			return filepath.Join(env["APPDATA"], "bilipdj")
		}
		return filepath.Join(home, "AppData", "Roaming", "bilipdj")
	}
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "bilipdj")
	}
	base := env["XDG_DATA_HOME"]
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "bilipdj")
}
func LogRoot(goos, home string, env map[string]string) string {
	if goos == "windows" {
		if env["LOCALAPPDATA"] != "" {
			return filepath.Join(env["LOCALAPPDATA"], "bilipdj", "log")
		}
		return filepath.Join(home, "AppData", "Local", "bilipdj", "log")
	}
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Logs", "bilipdj")
	}
	base := env["XDG_STATE_HOME"]
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "bilipdj", "log")
}
func ExistingUserData(root string) bool {
	files := []string{"state.json", "core/config.yaml", "core/quanxian.yaml", "core/kaiguan.yaml", "core/blacklist.csv", "core/style-web.json", "style-web.json", "appearance-web.json", "style-win.json", "appearance-win.json", "config.yaml", "style.json", "appearance.json"}
	for _, p := range files {
		if f, e := os.Lstat(filepath.Join(root, p)); e == nil && f.Mode().IsRegular() {
			return true
		}
	}
	for _, rel := range []string{"core/cd", "plugins", "key", "backup"} {
		folder := filepath.Join(root, rel)
		if f, e := os.Lstat(folder); e == nil && f.IsDir() {
			entries, _ := os.ReadDir(folder)
			for _, entry := range entries {
				if entry.Name() != ".gitkeep" && !strings.HasPrefix(entry.Name(), ".bilipdj-appdata-sync") {
					return true
				}
			}
		}
	}
	return false
}
func envNow() map[string]string {
	e := map[string]string{}
	for _, k := range []string{"APPDATA", "LOCALAPPDATA", "XDG_DATA_HOME", "XDG_STATE_HOME", "BILIPDJ_DATA_DIR", "BILIPDJ_DATA_CHOICE"} {
		e[k] = os.Getenv(k)
	}
	return e
}
func Resolve(appDir, legacyData, explicit string) (Plan, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return Plan{}, e
	}
	return PlanFor(appDir, legacyData, explicit, runtime.GOOS, home, envNow())
}
func PlanFor(appDir, legacyData, explicit, goos, home string, env map[string]string) (Plan, error) {
	var p Plan
	appDir, _ = filepath.Abs(appDir)
	legacyData, _ = filepath.Abs(legacyData)
	p.Legacy = legacyData
	p.User = UserRoot(goos, home, env)
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			explicit = filepath.Join(appDir, explicit)
		}
		p.Active = filepath.Clean(explicit)
		p.Mode = "explicit"
		p.Choice = "explicit"
		return p, nil
	}
	p.Mode = "managed"
	p.User, _ = filepath.Abs(p.User)
	if p.User == appDir || p.User == legacyData || strings.HasPrefix(p.User, appDir+string(os.PathSeparator)) || strings.HasPrefix(appDir, p.User+string(os.PathSeparator)) {
		return p, errors.New("system user directory must not overlap application directory")
	}
	// Legacy Python's project root, and the former Go ./data directory.
	p.OldHasData = ExistingUserData(legacyData) || ExistingUserData(appDir)
	p.NewHasData = ExistingUserData(p.User)
	choice := strings.ToLower(strings.TrimSpace(env["BILIPDJ_DATA_CHOICE"]))
	if choice != "legacy" && choice != "user" {
		choice = readChoice(p.User)
	}
	p.Choice = choice
	p.Conflict = p.OldHasData && p.NewHasData && choice == ""
	p.Active = p.User
	if p.Conflict || choice == "legacy" {
		p.Active = p.Legacy
	}
	p.Migrate = p.OldHasData && !p.NewHasData && p.Active == p.User
	return p, nil
}
func readChoice(user string) string {
	data, e := os.ReadFile(filepath.Join(user, choiceFile))
	if e != nil {
		return ""
	}
	var v struct {
		Choice string `json:"choice"`
	}
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	if v.Choice == "legacy" || v.Choice == "user" {
		return v.Choice
	}
	return ""
}
func Choose(p Plan, choice string) error {
	if p.Mode != "managed" {
		return errors.New("explicit data directory cannot be changed via UI")
	}
	if choice != "legacy" && choice != "user" {
		return errors.New("choice must be legacy or user")
	}
	return saveChoice(p.User, choice)
}
func saveChoice(user, choice string) error {
	if e := os.MkdirAll(user, 0700); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(user, ".storage-choice-*")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	_ = tmp.Chmod(0600)
	raw, _ := json.Marshal(map[string]any{"schema": 1, "choice": choice})
	raw = append(raw, '\n')
	if _, e = tmp.Write(raw); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Sync(); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	return os.Rename(tmp.Name(), filepath.Join(user, choiceFile))
}

// CopyOnly migrates a fixed allowlist; it cannot follow symlinked paths or
// overwrite targets. It pins the new directory only after successful copies.
func CopyOnly(p Plan, appDir string) (int, error) {
	if !p.Migrate || p.Mode != "managed" {
		return 0, nil
	}
	count := 0
	copyOne := func(src, dst string) error {
		info, e := os.Lstat(src)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		// Never follow a symlink in any component below one of the trusted roots.
		if !safeComponents(src, appDir, p.Legacy) {
			return nil
		}
		if !safeComponents(dst, p.User) {
			return fmt.Errorf("unsafe destination parent for %s", dst)
		}
		if info.Size() > 64<<20 {
			return fmt.Errorf("migration file too large: %s", src)
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		to, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		from, e := os.Open(src)
		if e != nil {
			to.Close()
			os.Remove(dst)
			return e
		}
		_, e = io.Copy(to, from)
		from.Close()
		closeErr := to.Close()
		if e != nil || closeErr != nil {
			os.Remove(dst)
			if e != nil {
				return e
			}
			return closeErr
		}
		count++
		return nil
	}
	// Old Go root: only Go-owned files are transferred, not executable assets.
	for _, name := range []string{"state.json"} {
		if e := copyOne(filepath.Join(p.Legacy, name), filepath.Join(p.User, name)); e != nil {
			return count, e
		}
	}
	// Python's original project layout, preserved exactly under the common root.
	files := []string{"core/config.yaml", "core/quanxian.yaml", "core/kaiguan.yaml", "core/blacklist.csv", "core/webdav_backup.json", "core/gift_compatibility.json", "core/language.json", "core/style.json", "core/appearance.json", "core/style-web.json", "core/style-win.json", "core/appearance-web.json", "core/appearance-win.json", "config.yaml", "quanxian.yaml", "kaiguan.yaml", "blacklist.csv", "style.json", "appearance.json", "style-web.json", "style-win.json", "appearance-web.json", "appearance-win.json"}
	for _, rel := range files {
		if e := copyOne(filepath.Join(appDir, rel), filepath.Join(p.User, rel)); e != nil {
			return count, e
		}
	}
	for _, dir := range []string{"core/cd", "plugins", "key", "backup"} {
		root := filepath.Join(appDir, dir)
		if info, e := os.Lstat(root); e != nil || !info.IsDir() {
			continue
		}
		e := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(appDir, path)
			if err != nil {
				return err
			}
			return copyOne(path, filepath.Join(p.User, rel))
		})
		if e != nil {
			return count, e
		}
	}
	if e := saveChoice(p.User, "user"); e != nil {
		return count, fmt.Errorf("migrated data but failed to record choice: %w", e)
	}
	return count, nil
}

// safeComponents checks existing path elements for symlinks. A destination
// component that does not exist is safe to create; existing junctions are not.
func safeComponents(candidate string, roots ...string) bool {
	for _, root := range roots {
		root, _ = filepath.Abs(root)
		candidate, _ = filepath.Abs(candidate)
		rel, e := filepath.Rel(root, candidate)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		current := root
		if info, e := os.Lstat(root); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		for _, segment := range strings.Split(rel, string(os.PathSeparator)) {
			if segment == "." {
				continue
			}
			current = filepath.Join(current, segment)
			info, e := os.Lstat(current)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
		}
		return true
	}
	return false
}
