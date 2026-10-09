//go:build !windows

package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunHelperBackupAndRestart(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bilipdj-go")
	backup := target + ".backup-0.10.0"
	staged := filepath.Join(dir, "staged")
	marker := filepath.Join(dir, "restarted")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("#!/bin/sh\nprintf restarted > \""+marker+"\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	job := InstallJob{Target: target, Backup: backup, Staged: staged, ZIP: filepath.Join(dir, "fake.zip"), ParentPID: 99999999, Dir: dir, Version: "v0.10.0"}
	payload, _ := json.Marshal(job)
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunHelper(jobPath); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(backup)
	if err != nil || !strings.Contains(string(old), "exit 0") {
		t.Fatalf("old executable not preserved: %v", err)
	}
	for i := 0; i < 30; i++ {
		if b, err := os.ReadFile(marker); err == nil && string(b) == "restarted" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("updated executable not launched")
}
