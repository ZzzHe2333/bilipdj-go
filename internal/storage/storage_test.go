package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestUserRootsAndLogs(t *testing.T) {
	h := t.TempDir()
	windows := UserRoot("windows", h, map[string]string{})
	if windows != filepath.Join(h, "AppData", "Roaming", "bilipdj") {
		t.Fatal(windows)
	}
	if v := LogRoot("windows", h, map[string]string{}); v != filepath.Join(h, "AppData", "Local", "bilipdj", "log") {
		t.Fatal(v)
	}
	if v := UserRoot("darwin", h, nil); v != filepath.Join(h, "Library", "Application Support", "bilipdj") {
		t.Fatal(v)
	}
	if v := LogRoot("darwin", h, nil); v != filepath.Join(h, "Library", "Logs", "bilipdj") {
		t.Fatal(v)
	}
	if v := UserRoot("linux", h, nil); v != filepath.Join(h, ".local", "share", "bilipdj") {
		t.Fatal(v)
	}
	if v := LogRoot("linux", h, nil); v != filepath.Join(h, ".local", "state", "bilipdj", "log") {
		t.Fatal(v)
	}
	if v := UserRoot("linux", h, map[string]string{"XDG_DATA_HOME": "relative"}); v != filepath.Join(h, ".local", "share", "bilipdj") {
		t.Fatal(v)
	}
	xdg := filepath.Join(h, "custom")
	if v := UserRoot("linux", h, map[string]string{"XDG_DATA_HOME": xdg}); v != filepath.Join(xdg, "bilipdj") {
		t.Fatal(v)
	}
	if v := LogRoot("linux", h, map[string]string{"XDG_STATE_HOME": xdg}); v != filepath.Join(xdg, "bilipdj", "log") {
		t.Fatal(v)
	}
}
func TestCopyOnlyAndConflictChoice(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "portable")
	old := filepath.Join(app, "data")
	new := filepath.Join(home, "xdg", "bilipdj")
	writeTestFile(t, filepath.Join(old, "state.json"), `{"queue":[{"username":"Old"}]}`)
	writeTestFile(t, filepath.Join(app, "core", "cd", "queue_archive_slot_2.csv"), "Python,douyin\n")
	writeTestFile(t, filepath.Join(app, "plugins", "data.json"), "test")
	writeTestFile(t, filepath.Join(app, "apps", "script.py"), "never-copy")
	env := map[string]string{"XDG_DATA_HOME": filepath.Dir(new)}
	p, e := PlanFor(app, old, "", "linux", home, env)
	if e != nil || !p.Migrate || p.Active != new {
		t.Fatalf("plan=%+v err=%v", p, e)
	}
	n, e := CopyOnly(p, app)
	if e != nil || n != 3 {
		t.Fatalf("copied=%d err=%v", n, e)
	}
	for _, f := range []string{"state.json", "archives/queue_archive_slot_2.csv", "plugins/data.json"} {
		if _, e := os.Stat(filepath.Join(new, f)); e != nil {
			t.Error(f, e)
		}
	}
	if _, e := os.Stat(filepath.Join(new, "apps", "script.py")); !os.IsNotExist(e) {
		t.Fatal("program copied")
	}
	p, e = PlanFor(app, old, "", "linux", home, env)
	if e != nil || p.Conflict || p.Active != new || p.Choice != "user" {
		t.Fatalf("postcopy=%+v err=%v", p, e)
	}
	writeTestFile(t, filepath.Join(new, "state.json"), "new")
	if e := os.Remove(filepath.Join(new, choiceFile)); e != nil {
		t.Fatal(e)
	}
	p, e = PlanFor(app, old, "", "linux", home, env)
	if e != nil || !p.Conflict || p.Active != old {
		t.Fatalf("conflict=%+v err=%v", p, e)
	}
	if e := Choose(p, "user"); e != nil {
		t.Fatal(e)
	}
	q, _ := PlanFor(app, old, "", "linux", home, env)
	if q.Conflict || q.Active != new {
		t.Fatal(q)
	}
	if e := Choose(p, "legacy"); e != nil {
		t.Fatal(e)
	}
	q, _ = PlanFor(app, old, "", "linux", home, env)
	if q.Conflict || q.Active != old {
		t.Fatal(q)
	}
	raw, _ := os.ReadFile(filepath.Join(new, "state.json"))
	if string(raw) != "new" {
		t.Fatalf("overwrote target: %q", raw)
	}
	raw, _ = os.ReadFile(filepath.Join(old, "state.json"))
	if !strings.Contains(string(raw), "Old") {
		t.Fatal("overwrote original")
	}
	explicit, _ := PlanFor(app, old, filepath.Join(home, "docker"), "linux", home, env)
	if explicit.Mode != "explicit" || explicit.Migrate || explicit.Conflict || explicit.Active != filepath.Join(home, "docker") {
		t.Fatal(explicit)
	}
	if e := Choose(explicit, "user"); e == nil {
		t.Fatal("explicit mode should reject choice")
	}
}
func TestMigrationSkipsSymlinks(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "portable")
	old := filepath.Join(app, "data")
	new := filepath.Join(home, "xdg", "bilipdj")
	writeTestFile(t, filepath.Join(old, "state.json"), "legacy")
	outside := filepath.Join(home, "outside.txt")
	writeTestFile(t, outside, "private")
	if e := os.MkdirAll(filepath.Join(app, "core"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(app, "core", "config.yaml")); e != nil {
		t.Skip(e)
	}
	p, e := PlanFor(app, old, "", "linux", home, map[string]string{"XDG_DATA_HOME": filepath.Dir(new)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = CopyOnly(p, app); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(filepath.Join(new, "core", "config.yaml")); !os.IsNotExist(e) {
		t.Fatal("symlink source should not migrate")
	}
}

func TestPR314StorageLocationsAndSafeMigration(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local")}
	app := filepath.Join(home, "portable")
	old := filepath.Join(app, "data")
	roaming := UserRoot("windows", home, env)
	local := StateRoot("windows", home, env)
	writeTestFile(t, filepath.Join(old, "state.json"), `{"config": {"archive_slot":1}}`)
	writeTestFile(t, filepath.Join(app, "core", "cd", "queue_archive_slot_1.csv"), "old-queue")
	writeTestFile(t, filepath.Join(app, "backup", "old.zip"), "old-backup")
	p, err := PlanFor(app, old, "", "windows", home, env)
	if err != nil || !p.Migrate {
		t.Fatalf("plan %+v %v", p, err)
	}
	if p.ArchiveDir != filepath.Join(local, "archives") || p.BackupDir != filepath.Join(local, "backups") || p.CacheDir != filepath.Join(local, "cache") || p.LogDir != filepath.Join(local, "log") {
		t.Fatalf("unexpected Windows paths %+v", p)
	}
	if _, err = CopyOnly(p, app); err != nil {
		t.Fatal(err)
	}
	if _, err = MigrateLocalState(&p, app); err != nil {
		t.Fatal(err)
	}
	for dest, want := range map[string]string{
		filepath.Join(roaming, "state.json"):                         `{"config": {"archive_slot":1}}`,
		filepath.Join(local, "archives", "queue_archive_slot_1.csv"): "old-queue",
		filepath.Join(local, "backups", "old.zip"):                   "old-backup",
		filepath.Join(app, "core", "cd", "queue_archive_slot_1.csv"): "old-queue",
	} {
		if b, err := os.ReadFile(dest); err != nil || string(b) != want {
			t.Fatalf("%s: %q %v", dest, b, err)
		}
	}
	// A newer queue in Local must not be replaced by stale Roaming or portable snapshots.
	writeTestFile(t, filepath.Join(local, "archives", "queue_archive_slot_1.csv"), "new-live-queue")
	if _, err = MigrateLocalState(&p, app); err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) == 0 {
		t.Fatal("expected divergent old and new archive to be reported")
	}
	b, _ := os.ReadFile(filepath.Join(local, "archives", "queue_archive_slot_1.csv"))
	if string(b) != "new-live-queue" {
		t.Fatal("overwrote current Local archive")
	}
	// Docker users continue using the exact explicit /data layout, no OS migration.
	docker, err := PlanFor(app, old, filepath.Join(home, "docker-volume"), "windows", home, env)
	if err != nil || docker.ArchiveDir != filepath.Join(home, "docker-volume", "core", "cd") || docker.BackupDir != filepath.Join(home, "docker-volume", "backup") {
		t.Fatalf("docker %+v %v", docker, err)
	}
	n, err := MigrateLocalState(&docker, app)
	if err != nil || n != 0 {
		t.Fatalf("Docker must not migrate: %d %v", n, err)
	}
	for _, goos := range []string{"darwin", "linux"} {
		env := map[string]string{"XDG_DATA_HOME": filepath.Join(home, "xdg")}
		plan, err := PlanFor(app, old, filepath.Join(home, "explicit"), goos, home, env)
		if err != nil || plan.Mode != "explicit" {
			t.Fatal(plan, err)
		}
		managedApp := filepath.Join(home, "newapp-"+goos)
		root, _ := PlanFor(managedApp, filepath.Join(managedApp, "data"), "", goos, home, env)
		if root.ArchiveDir != filepath.Join(root.User, "archives") || root.BackupDir != filepath.Join(root.User, "backups") {
			t.Fatalf("%s %+v", goos, root)
		}
	}
}
