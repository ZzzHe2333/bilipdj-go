// Package storage keeps BiliPDJ Go user data physically separate from Python.
// Only recognizable Go-owned state is copied from older shared installations.
package storage

import (
	"bytes"
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
const goFolder = "bilipdj-go"
const pythonFolder = "bilipdj"

type Plan struct {
	Mode               string   `json:"mode"`
	Legacy             string   `json:"legacy"`
	User               string   `json:"user"`
	Active             string   `json:"active"`
	Choice             string   `json:"choice"`
	Conflict           bool     `json:"conflict"`
	Migrate            bool     `json:"migrate"`
	OldHasData         bool     `json:"old_has_data"`
	NewHasData         bool     `json:"new_has_data"`
	ArchiveDir         string   `json:"archive_dir"`
	BackupDir          string   `json:"backup_dir"`
	CacheDir           string   `json:"cache_dir"`
	LogDir             string   `json:"log_dir"`
	PythonArchiveDir   string   `json:"python_archive_dir,omitempty"`
	PreviousGoRoot     string   `json:"previous_go_root,omitempty"`
	PreviousGoArchives string   `json:"previous_go_archives,omitempty"`
	Conflicts          []string `json:"local_conflicts,omitempty"`
}

func userRootNamed(goos, home string, env map[string]string, folder string) string {
	switch goos {
	case "windows":
		root := env["APPDATA"]
		if root == "" {
			root = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(root, folder)
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", folder)
	default:
		root := env["XDG_DATA_HOME"]
		if !filepath.IsAbs(root) {
			root = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(root, folder)
	}
}
func stateRootNamed(goos, home string, env map[string]string, folder string) string {
	if goos == "windows" {
		base := env["LOCALAPPDATA"]
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, folder)
	}
	return userRootNamed(goos, home, env, folder)
}
func logRootNamed(goos, home string, env map[string]string, folder string) string {
	switch goos {
	case "windows":
		return filepath.Join(stateRootNamed(goos, home, env, folder), "log")
	case "darwin":
		return filepath.Join(home, "Library", "Logs", folder)
	default:
		root := env["XDG_STATE_HOME"]
		if !filepath.IsAbs(root) {
			root = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(root, folder, "log")
	}
}
func UserRoot(goos, home string, env map[string]string) string {
	return userRootNamed(goos, home, env, goFolder)
}
func StateRoot(goos, home string, env map[string]string) string {
	return stateRootNamed(goos, home, env, goFolder)
}
func LogRoot(goos, home string, env map[string]string) string {
	return logRootNamed(goos, home, env, goFolder)
}
func setPaths(p *Plan, goos, home string, env map[string]string) {
	if p.Mode == "explicit" {
		p.ArchiveDir = filepath.Join(p.Active, "core", "cd")
		p.BackupDir = filepath.Join(p.Active, "backup")
		p.CacheDir = filepath.Join(p.Active, "cache")
		p.LogDir = filepath.Join(p.Active, "log")
		return
	}
	p.ArchiveDir = filepath.Join(StateRoot(goos, home, env), "archives")
	p.BackupDir = filepath.Join(StateRoot(goos, home, env), "backups")
	p.CacheDir = filepath.Join(StateRoot(goos, home, env), "cache")
	p.LogDir = LogRoot(goos, home, env)
	p.PythonArchiveDir = filepath.Join(stateRootNamed(goos, home, env, pythonFolder), "archives")
	p.PreviousGoArchives = "" // Set only for a verified Go state in the old shared user directory.
}

// A Go state must have its config object. Python-owned files and empty folders
// never trigger migration into the new Go directory.
func goState(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, false, nil
	}
	var config map[string]json.RawMessage
	if c, ok := fields["config"]; ok && json.Unmarshal(c, &config) == nil && config != nil {
		return raw, true, nil
	}
	return nil, false, nil
}

func ExistingUserData(root string) bool {
	_, ok, _ := goState(filepath.Join(root, "state.json"))
	return ok
}
func envNow() map[string]string {
	e := map[string]string{}
	for _, k := range []string{"APPDATA", "LOCALAPPDATA", "XDG_DATA_HOME", "XDG_STATE_HOME", "BILIPDJ_DATA_DIR", "BILIPDJ_DATA_CHOICE"} {
		e[k] = os.Getenv(k)
	}
	return e
}
func Resolve(appDir, legacyData, explicit string) (Plan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Plan{}, err
	}
	return PlanFor(appDir, legacyData, explicit, runtime.GOOS, home, envNow())
}
func PlanFor(appDir, legacyData, explicit, goos, home string, env map[string]string) (Plan, error) {
	var p Plan
	var err error
	appDir, err = filepath.Abs(appDir)
	if err != nil {
		return p, err
	}
	legacyData, err = filepath.Abs(legacyData)
	if err != nil {
		return p, err
	}
	p.Legacy = legacyData
	p.User = UserRoot(goos, home, env)
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			explicit = filepath.Join(appDir, explicit)
		}
		p.Active = filepath.Clean(explicit)
		p.Mode, p.Choice = "explicit", "explicit"
		setPaths(&p, goos, home, env)
		return p, nil
	}
	p.Mode, p.Choice = "managed", "user"
	p.User, err = filepath.Abs(p.User)
	if err != nil {
		return p, err
	}
	if p.User == appDir || p.User == legacyData || strings.HasPrefix(p.User, appDir+string(os.PathSeparator)) || strings.HasPrefix(appDir, p.User+string(os.PathSeparator)) {
		return p, errors.New("Go user directory must be separate from application directory")
	}
	p.Active = p.User // Never write to Python's bilipdj root, even on conflict.
	setPaths(&p, goos, home, env)
	previous := userRootNamed(goos, home, env, pythonFolder)
	candidates := []string{previous, legacyData}
	var selected []byte
	for _, path := range candidates {
		raw, ok, err := goState(filepath.Join(path, "state.json"))
		if err != nil {
			return p, err
		}
		if !ok {
			// Both candidate paths are historically reserved for Go's
			// state.json; a malformed copy must not silently produce an empty
			// installation or discard the user's login and queue.
			if _, statErr := os.Lstat(filepath.Join(path, "state.json")); statErr == nil {
				return p, fmt.Errorf("old Go state.json at %s is invalid or unsafe; back it up before migration", path)
			}
			continue
		}
		if selected != nil && !bytes.Equal(selected, raw) {
			return p, fmt.Errorf("multiple different old Go state.json files (%s, %s); back up and select one manually", p.PreviousGoRoot, path)
		}
		if selected == nil {
			p.PreviousGoRoot, selected = path, raw
		}
	}
	p.OldHasData = selected != nil
	if p.PreviousGoRoot == previous {
		p.PreviousGoArchives = p.PythonArchiveDir
	}
	existing, valid, err := goState(filepath.Join(p.User, "state.json"))
	if err != nil {
		return p, err
	}
	if !valid {
		if info, err := os.Lstat(filepath.Join(p.User, "state.json")); err == nil && info != nil {
			return p, fmt.Errorf("existing Go state.json is invalid; refusing to overwrite: %s", filepath.Join(p.User, "state.json"))
		}
	}
	p.NewHasData = valid
	if p.OldHasData && p.NewHasData && !bytes.Equal(existing, selected) {
		p.Conflict = readChoice(p.User) != "user"
		p.Conflicts = append(p.Conflicts, filepath.Join(p.User, "state.json"))
	}
	p.Migrate = p.OldHasData && !p.NewHasData
	return p, nil
}

// New installations never switch to the shared old Python directory.
func Choose(p Plan, choice string) error {
	if p.Mode != "managed" {
		return errors.New("explicit storage cannot be changed")
	}
	if choice != "user" {
		return errors.New("legacy bilipdj directory is reserved for Python; Go only uses bilipdj-go")
	}
	return saveChoice(p.User, choice)
}
func readChoice(root string) string {
	data, err := os.ReadFile(filepath.Join(root, choiceFile))
	if err != nil {
		return ""
	}
	var v struct {
		Choice string `json:"choice"`
	}
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	return v.Choice
}
func saveChoice(root, choice string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".storage-choice-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_ = f.Chmod(0600)
	data, _ := json.Marshal(map[string]any{"schema": 1, "choice": choice})
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(root, choiceFile))
}

// CopyOnly copies only proven Go-owned files. Python config, CSV, plugins,
// snapshots and shared storage-choice decisions are never imported implicitly.
func CopyOnly(p Plan, appDir string) (int, error) {
	if !p.Migrate || p.Mode != "managed" {
		return 0, nil
	}
	if p.PreviousGoRoot == "" {
		return 0, errors.New("old Go state source not resolved")
	}
	if _, ok, err := goState(filepath.Join(p.PreviousGoRoot, "state.json")); err != nil || !ok {
		return 0, errors.New("old Go state is no longer valid; stop migration")
	}
	count := 0
	for _, name := range []string{"style-web.json", "appearance-web.json", "state.json"} {
		src := filepath.Join(p.PreviousGoRoot, name)
		target := filepath.Join(p.User, name)
		copied, err := copyMissing(src, target)
		if err != nil {
			return count, err
		}
		if copied {
			count++
		}
	}
	// New Go storage is permanently authoritative; the old Python/shared
	// directory must never be selected by a leftover storage-choice file.
	if err := saveChoice(p.User, "user"); err != nil {
		return count, err
	}
	return count, nil
}

// Copy via O_EXCL, leaving both source and any previously-existing target intact.
func copyMissing(src, dst string) (bool, error) {
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return false, fmt.Errorf("unsafe Go migration source: %s", src)
	}
	if !safeComponents(src, filepath.Dir(src)) || !safeComponents(dst, filepath.Dir(dst)) {
		return false, fmt.Errorf("unsafe Go migration path: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return false, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		old, e := os.ReadFile(dst)
		if e != nil {
			return false, e
		}
		newer, e := os.ReadFile(src)
		if e != nil {
			return false, e
		}
		if !bytes.Equal(old, newer) {
			return false, fmt.Errorf("Go migration conflict: %s differs from %s (neither was overwritten)", src, dst)
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		os.Remove(dst)
		return false, err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if e := out.Sync(); err == nil {
		err = e
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(dst)
		return false, err
	}
	return true, nil
}
func safeComponents(candidate string, roots ...string) bool {
	for _, root := range roots {
		root, _ = filepath.Abs(root)
		candidate, _ = filepath.Abs(candidate)
		rel, err := filepath.Rel(root, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		walk := root
		if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		for _, seg := range strings.Split(rel, string(os.PathSeparator)) {
			if seg == "." {
				continue
			}
			walk = filepath.Join(walk, seg)
			info, err := os.Lstat(walk)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
		}
		return true
	}
	return false
}

// Copy older Go-specific queue and rolling backups, but not a single Python CSV.
// Call only before the new destination has been used or if files are missing.
func MigrateLocalState(p *Plan, _ string) (int, error) {
	if p.Mode != "managed" || p.Active != p.User {
		return 0, nil
	}
	for _, dir := range []string{p.ArchiveDir, p.BackupDir, p.CacheDir, p.LogDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return 0, err
		}
	}
	if !p.OldHasData || p.PreviousGoArchives == "" {
		return 0, nil
	}
	// If Go is already operating from the new directory, stale legacy copies
	// must never replace (or make startup fail over) a newer current queue.
	if !p.Migrate && (p.Conflict || !p.NewHasData) {
		return 0, nil
	}
	// A partial migration is resumable only while the new state is byte-for-byte
	// identical to the old state (no newer Go writes have occurred).
	if !p.Migrate {
		old, oldErr := os.ReadFile(filepath.Join(p.PreviousGoRoot, "state.json"))
		current, currentErr := os.ReadFile(filepath.Join(p.User, "state.json"))
		if oldErr != nil || currentErr != nil || !bytes.Equal(old, current) {
			return 0, nil
		}
	}
	oldState, err := os.ReadFile(filepath.Join(p.PreviousGoRoot, "state.json"))
	if err != nil {
		return 0, err
	}
	var oldMarker struct {
		QueueExternal bool `json:"queue_external"`
	}
	if err := json.Unmarshal(oldState, &oldMarker); err != nil {
		return 0, err
	}
	if !oldMarker.QueueExternal {
		return 0, nil
	}
	source := filepath.Join(p.PreviousGoArchives, "go-queue-state.json")
	target := filepath.Join(p.ArchiveDir, "go-queue-state.json")
	count := 0
	n, err := copyMissingIfPresent(source, target)
	if err != nil {
		return count, err
	}
	if n {
		count++
	}
	// Only Go-prefixed rolling CSVs are migrated. All Python archives and
	// Python backups stay under bilipdj.
	backupRoot := filepath.Join(filepath.Dir(p.PreviousGoArchives), "backups")
	for i := 1; i <= 10; i++ {
		sub := fmt.Sprintf("queue/queue_archive_slot_%d", i)
		dir := filepath.Join(backupRoot, filepath.FromSlash(sub))
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return count, err
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), "go-") || !strings.HasSuffix(entry.Name(), ".csv") {
				continue
			}
			src := filepath.Join(dir, entry.Name())
			dst := filepath.Join(p.BackupDir, filepath.FromSlash(sub), entry.Name())
			copied, err := copyMissingIfPresent(src, dst)
			if err != nil {
				return count, err
			}
			if copied {
				count++
			}
		}
	}
	return count, nil
}
func copyMissingIfPresent(src, dst string) (bool, error) {
	if _, err := os.Lstat(src); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return copyMissing(src, dst)
}
