package core

import (
	"encoding/json"
	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPR314SplitStateAndRollingQueueBackup(t *testing.T) {
	dir := t.TempDir()
	roaming := filepath.Join(dir, "roaming", "bilipdj")
	local := filepath.Join(dir, "local", "bilipdj")
	if err := os.MkdirAll(roaming, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := New(roaming, "old", "repo")
	legacy.queue = []QueueItem{{Username: "甲", Platform: "bilibili", Key: "bilibili:1", UserID: "1"}}
	legacy.config.Admins = []string{"manager"}
	if err := legacy.saveLocked(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(roaming, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	plan := storage.Plan{Mode: "managed", User: roaming, Active: roaming, ArchiveDir: filepath.Join(local, "archives"), BackupDir: filepath.Join(local, "backups")}
	app := New(roaming, "new", "repo")
	if err = app.SetStoragePlan(plan); err != nil {
		t.Fatal(err)
	}
	if app.updater.Dir != plan.CacheDir && plan.CacheDir != "" {
		t.Fatal("updater cache outside Local cache directory")
	}
	if len(app.queue) != 1 || app.queue[0].Platform != "bilibili" {
		t.Fatal("lost old Go queue")
	}
	archives := filepath.Join(local, "archives", "go-queue-state.json")
	if _, err = os.Stat(archives); err != nil {
		t.Fatal("initial Local copy missing", err)
	}
	baks, err := filepath.Glob(filepath.Join(local, "backups", "migration-backup", "go-state-before-split-*.json"))
	if err != nil || len(baks) != 1 {
		t.Fatalf("old state backup=%v error=%v", baks, err)
	}
	backup, err := os.ReadFile(baks[0])
	if err != nil || string(backup) != string(original) {
		t.Fatal("original pre-split Roaming state not preserved")
	}
	app.queue = []QueueItem{{Username: "乙", Platform: "douyin", Key: "douyin:2", UserID: "2"}}
	if err = app.saveLocked(); err != nil {
		t.Fatal(err)
	}
	latest := New(roaming, "new", "repo")
	if err = latest.SetStoragePlan(plan); err != nil {
		t.Fatal(err)
	}
	if len(latest.queue) != 1 || latest.queue[0].Platform != "douyin" || latest.config.Admins[0] != "manager" {
		t.Fatalf("reload wrong %+v %v", latest.queue, latest.config.Admins)
	}
	configBytes, err := os.ReadFile(filepath.Join(roaming, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var conf map[string]json.RawMessage
	if err = json.Unmarshal(configBytes, &conf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(conf["queue"]), "乙") || strings.Contains(string(configBytes), "douyin:2") {
		t.Fatal("queue content left in Roaming")
	}
	first, _ := filepath.Glob(filepath.Join(local, "backups", "queue", "queue_archive_slot_1", "go-*.csv"))
	if len(first) != 1 {
		t.Fatalf("expected one rolling prior snapshot; got %v", first)
	}
	prev, err := os.ReadFile(first[0])
	if err != nil || !strings.Contains(string(prev), "bilibili") {
		t.Fatal("backup must preserve previous platform")
	}
	latest.queue = []QueueItem{{Username: "丙", Platform: "bilibili", Key: "bilibili:3"}}
	if err = latest.saveLocked(); err != nil {
		t.Fatal(err)
	}
	second, _ := filepath.Glob(filepath.Join(local, "backups", "queue", "queue_archive_slot_1", "go-*.csv"))
	if len(second) != 1 {
		t.Fatalf("same 30-minute window should have exactly one backup; got %v", second)
	}
	// Once split, the marker prohibits a missing Local file from silently clearing queue.
	if err := os.Remove(archives); err != nil {
		t.Fatal(err)
	}
	missing := New(roaming, "new", "repo")
	if err = missing.SetStoragePlan(plan); err == nil {
		t.Fatal("missing authoritative Local archive must fail safe")
	}
	// Explicit Docker-style installations remain single-file and unchanged.
	docker := New(filepath.Join(dir, "docker"), "new", "repo")
	if err = docker.SetStoragePlan(storage.Plan{Mode: "explicit", Active: filepath.Join(dir, "docker")}); err != nil {
		t.Fatal(err)
	}
	docker.queue = []QueueItem{{Username: "Docker"}}
	if err = docker.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "docker", "state.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "docker", "core", "cd", "go-queue-state.json")); !os.IsNotExist(err) {
		t.Fatal("explicit mode should not split queue")
	}
}
func TestPR314CorruptedLocalQueueIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "archives", "go-queue-state.json")
	if err := os.MkdirAll(filepath.Dir(bad), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(bad, []byte("not JSON"), 0600)
	plan := storage.Plan{Mode: "managed", Active: dir, User: dir, ArchiveDir: filepath.Dir(bad), BackupDir: filepath.Join(dir, "backups")}
	a := New(dir, "new", "repo")
	if err := a.SetStoragePlan(plan); err == nil {
		t.Fatal("corrupt Go local queue must stop startup")
	}
	original, _ := os.ReadFile(bad)
	if string(original) != "not JSON" {
		t.Fatal("corrupt archive overwritten")
	}
}
