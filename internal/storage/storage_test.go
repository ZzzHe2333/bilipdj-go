package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func getTestPlan(t *testing.T, goos, home, app string, env map[string]string) Plan {
	t.Helper()
	p, err := PlanFor(app, filepath.Join(app, "data"), "", goos, home, env)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestIsolatedRootsAllOperatingSystems(t *testing.T) {
	h := t.TempDir()
	env := map[string]string{"APPDATA": filepath.Join(h, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(h, "AppData", "Local"), "XDG_DATA_HOME": filepath.Join(h, "xdg"), "XDG_STATE_HOME": filepath.Join(h, "state")}
	tests := []struct{ os, root, state, log string }{
		{"windows", filepath.Join(env["APPDATA"], "bilipdj-go"), filepath.Join(env["LOCALAPPDATA"], "bilipdj-go"), filepath.Join(env["LOCALAPPDATA"], "bilipdj-go", "log")},
		{"darwin", filepath.Join(h, "Library", "Application Support", "bilipdj-go"), filepath.Join(h, "Library", "Application Support", "bilipdj-go"), filepath.Join(h, "Library", "Logs", "bilipdj-go")},
		{"linux", filepath.Join(env["XDG_DATA_HOME"], "bilipdj-go"), filepath.Join(env["XDG_DATA_HOME"], "bilipdj-go"), filepath.Join(env["XDG_STATE_HOME"], "bilipdj-go", "log")},
	}
	for _, tc := range tests {
		if got := UserRoot(tc.os, h, env); got != tc.root {
			t.Errorf("%s user %s", tc.os, got)
		}
		if got := StateRoot(tc.os, h, env); got != tc.state {
			t.Errorf("%s state %s", tc.os, got)
		}
		if got := LogRoot(tc.os, h, env); got != tc.log {
			t.Errorf("%s log %s", tc.os, got)
		}
		if strings.Contains(filepath.Base(UserRoot(tc.os, h, env)), "bilipdj/") {
			t.Fatal("bad root")
		}
	}
	if got := UserRoot("linux", h, map[string]string{"XDG_DATA_HOME": "relative"}); got != filepath.Join(h, ".local", "share", "bilipdj-go") {
		t.Fatal(got)
	}
	if got := LogRoot("linux", h, nil); got != filepath.Join(h, ".local", "state", "bilipdj-go", "log") {
		t.Fatal(got)
	}
	if got := UserRoot("windows", h, nil); got != filepath.Join(h, "AppData", "Roaming", "bilipdj-go") {
		t.Fatal(got)
	}
	if got := LogRoot("windows", h, nil); got != filepath.Join(h, "AppData", "Local", "bilipdj-go", "log") {
		t.Fatal(got)
	}
}
func TestPythonOnlyNeverMigrated(t *testing.T) {
	h := t.TempDir()
	app := filepath.Join(h, "app")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(h, "xdg")}
	py := userRootNamed("linux", h, env, pythonFolder)
	writeTestFile(t, filepath.Join(py, "core", "config.yaml"), "python-settings")
	writeTestFile(t, filepath.Join(py, "archives", "queue_archive_slot_1.csv"), "python-queue")
	writeTestFile(t, filepath.Join(app, "core", "cd", "queue_archive_slot_2.csv"), "python-portable")
	p := getTestPlan(t, "linux", h, app, env)
	if p.OldHasData || p.Migrate || p.Conflict || p.User == py || p.Active != p.User {
		t.Fatal(p)
	}
	if _, e := CopyOnly(p, app); e != nil {
		t.Fatal(e)
	}
	if _, e := MigrateLocalState(&p, app); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(p.User, "core", "config.yaml")); !os.IsNotExist(e) {
		t.Fatal("Python config migrated")
	}
	if _, e := os.Stat(filepath.Join(p.ArchiveDir, "queue_archive_slot_1.csv")); !os.IsNotExist(e) {
		t.Fatal("Python queue migrated")
	}
}
func TestGoOwnedMigrationOnly(t *testing.T) {
	h := t.TempDir()
	app := filepath.Join(h, "app")
	env := map[string]string{"APPDATA": filepath.Join(h, "Roaming"), "LOCALAPPDATA": filepath.Join(h, "Local")}
	old := userRootNamed("windows", h, env, pythonFolder)
	oldLocal := stateRootNamed("windows", h, env, pythonFolder)
	oldState := `{"config":{"archive_slot":1},"queue_external":true}`
	queue := `{"schema":1,"queue":[{"username":"Alice","platform":"douyin"}],"slots":{"1":[{"username":"Alice","platform":"douyin"}]}}`
	writeTestFile(t, filepath.Join(old, "state.json"), oldState)
	writeTestFile(t, filepath.Join(old, "core", "config.yaml"), "python-only")
	writeTestFile(t, filepath.Join(old, "style-web.json"), `{"custom":"go-style"}`)
	writeTestFile(t, filepath.Join(old, "style-win.json"), `{"python":"tk"}`)
	writeTestFile(t, filepath.Join(oldLocal, "archives", "go-queue-state.json"), queue)
	writeTestFile(t, filepath.Join(oldLocal, "archives", "queue_archive_slot_1.csv"), "python,do-not-touch")
	writeTestFile(t, filepath.Join(oldLocal, "backups", "queue", "queue_archive_slot_1", "go-202610091300.csv"), "old-Go-snapshot")
	writeTestFile(t, filepath.Join(oldLocal, "backups", "queue", "queue_archive_slot_1", "202610091300.csv"), "old-Python-snapshot")
	p := getTestPlan(t, "windows", h, app, env)
	if !p.Migrate || p.Active != p.User || p.PreviousGoRoot != old {
		t.Fatal(p)
	}
	n, err := CopyOnly(p, app)
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	n, err = MigrateLocalState(&p, app)
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	for _, v := range []struct{ p, data string }{
		{filepath.Join(p.User, "state.json"), oldState},
		{filepath.Join(p.User, "style-web.json"), `{"custom":"go-style"}`},
		{filepath.Join(p.ArchiveDir, "go-queue-state.json"), queue},
		{filepath.Join(oldLocal, "archives", "queue_archive_slot_1.csv"), "python,do-not-touch"},
		{filepath.Join(p.BackupDir, "queue", "queue_archive_slot_1", "go-202610091300.csv"), "old-Go-snapshot"},
	} {
		raw, e := os.ReadFile(v.p)
		if e != nil || string(raw) != v.data {
			t.Fatalf("%s: %q %v", v.p, raw, e)
		}
	}
	for _, path := range []string{filepath.Join(p.User, "core", "config.yaml"), filepath.Join(p.User, "style-win.json"), filepath.Join(p.ArchiveDir, "queue_archive_slot_1.csv"), filepath.Join(p.BackupDir, "queue", "queue_archive_slot_1", "202610091300.csv")} {
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			t.Fatalf("Python-owned file appeared %s", path)
		}
	}
	after := getTestPlan(t, "windows", h, app, env)
	if after.Migrate || after.Conflict || after.Active != after.User {
		t.Fatal(after)
	}
	writeTestFile(t, filepath.Join(p.User, "state.json"), `{"config":{"archive_slot":2},"queue_external":true}`)
	updated := getTestPlan(t, "windows", h, app, env)
	if updated.Conflict || updated.Active != updated.User {
		t.Fatal("migrated choice should pin new Go directory", updated)
	}
	if err := os.Remove(filepath.Join(updated.User, choiceFile)); err != nil {
		t.Fatal(err)
	}
	updated = getTestPlan(t, "windows", h, app, env)
	if !updated.Conflict {
		t.Fatal("divergent original and new Go data should be reported without pinned choice")
	}
	if e := Choose(updated, "legacy"); e == nil {
		t.Fatal("cannot choose Python directory")
	}
	if e := Choose(updated, "user"); e != nil {
		t.Fatal(e)
	}
	acknowledged := getTestPlan(t, "windows", h, app, env)
	if acknowledged.Conflict || acknowledged.Active != p.User {
		t.Fatal(acknowledged)
	}
}
func TestDivergentOldGoSourcesStopMigration(t *testing.T) {
	h := t.TempDir()
	app := filepath.Join(h, "app")
	old := filepath.Join(app, "data")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(h, "xdg")}
	py := userRootNamed("linux", h, env, pythonFolder)
	writeTestFile(t, filepath.Join(old, "state.json"), `{"config":{"command":"old"}}`)
	writeTestFile(t, filepath.Join(py, "state.json"), `{"config":{"command":"other"}}`)
	if _, err := PlanFor(app, old, "", "linux", h, env); err == nil {
		t.Fatal("divergent old Go states must be rejected")
	}
}
func TestInterruptedGoMigrationResumesOnlyUnmodified(t *testing.T) {
	h := t.TempDir()
	app := filepath.Join(h, "app")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(h, "xdg")}
	old := userRootNamed("linux", h, env, pythonFolder)
	writeTestFile(t, filepath.Join(old, "state.json"), `{"config":{"archive_slot":1},"queue_external":true}`)
	writeTestFile(t, filepath.Join(old, "archives", "go-queue-state.json"), `{"schema":1,"queue":[],"slots":{"1":[]}}`)
	p := getTestPlan(t, "linux", h, app, env)
	if _, err := CopyOnly(p, app); err != nil {
		t.Fatal(err)
	}
	p2 := getTestPlan(t, "linux", h, app, env)
	if p2.Migrate || p2.Conflict {
		t.Fatal(p2)
	}
	if _, err := MigrateLocalState(&p2, app); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p2.ArchiveDir, "go-queue-state.json")); err != nil {
		t.Fatal("interrupted migration not resumed", err)
	}
	// A subsequent updated Go state must not trigger migration from stale Python root.
	os.Remove(filepath.Join(p2.ArchiveDir, "go-queue-state.json"))
	writeTestFile(t, filepath.Join(p2.User, "state.json"), `{"config":{"archive_slot":2},"queue_external":true}`)
	newer := getTestPlan(t, "linux", h, app, env)
	if _, err := MigrateLocalState(&newer, app); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newer.ArchiveDir, "go-queue-state.json")); !os.IsNotExist(err) {
		t.Fatal("stale old queue copied into newer Go install")
	}
}
func TestExplicitDockerAndSymlinkGuard(t *testing.T) {
	h := t.TempDir()
	app := filepath.Join(h, "app")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(h, "xdg")}
	volume := filepath.Join(h, "docker")
	p, err := PlanFor(app, filepath.Join(app, "data"), volume, "linux", h, env)
	if err != nil || p.Mode != "explicit" || p.Active != volume || p.ArchiveDir != filepath.Join(volume, "core", "cd") || p.BackupDir != filepath.Join(volume, "backup") || p.Migrate {
		t.Fatal(p, err)
	}
	if _, e := MigrateLocalState(&p, app); e != nil {
		t.Fatal(e)
	}
	if e := Choose(p, "user"); e == nil {
		t.Fatal("explicit directory should not be changeable")
	}
	// Never dereference a symlinked Go source file.
	outside := filepath.Join(h, "outside.json")
	writeTestFile(t, outside, `{"config":{"cookie":"private"}}`)
	old := userRootNamed("linux", h, env, pythonFolder)
	if e := os.MkdirAll(old, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(old, "state.json")); e != nil {
		t.Skip(e)
	}
	if _, err := PlanFor(app, filepath.Join(app, "data"), "", "linux", h, env); err == nil {
		t.Fatal("symlinked old Go state should abort migration")
	}
}

func TestCorruptOldGoStateDoesNotStartEmpty(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "app")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(home, "xdg")}
	py := userRootNamed("linux", home, env, pythonFolder)
	writeTestFile(t, filepath.Join(py, "state.json"), `{"config":`)
	if _, err := PlanFor(app, filepath.Join(app, "data"), "", "linux", home, env); err == nil {
		t.Fatal("malformed old Go state must not be ignored")
	}
}
func TestPortableGoMigrationWithoutOldPythonArchive(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "app")
	old := filepath.Join(app, "data")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(home, "xdg")}
	writeTestFile(t, filepath.Join(old, "state.json"), `{"config":{"command":"排队"},"queue":[{"username":"Old"}]}`)
	py := userRootNamed("linux", home, env, pythonFolder)
	writeTestFile(t, filepath.Join(py, "archives", "go-queue-state.json"), `{"schema":1,"slots":{"1":[{"username":"Stale"}]}}`)
	p := getTestPlan(t, "linux", home, app, env)
	if p.PreviousGoRoot != old || p.PreviousGoArchives != "" {
		t.Fatal("portable import must not pick unrelated old shared queue", p)
	}
	if _, err := CopyOnly(p, app); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateLocalState(&p, app); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.ArchiveDir, "go-queue-state.json")); !os.IsNotExist(err) {
		t.Fatal("stale queue from Python root was imported")
	}
	if data, err := os.ReadFile(filepath.Join(p.User, "state.json")); err != nil || !strings.Contains(string(data), "Old") {
		t.Fatal("portable Go queue not retained")
	}
}
