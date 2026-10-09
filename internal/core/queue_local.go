package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
)

type localQueue struct {
	Schema int                    `json:"schema"`
	Queue  []QueueItem            `json:"queue"`
	Slots  map[string][]QueueItem `json:"slots"`
}

// Go's authoritative queue is independent of Python's archives/*.csv.
// The old all-in-one state.json remains readable for seamless upgrades.
func (a *App) configureLocalQueue(plan storage.Plan) error {
	if plan.Mode != "managed" || plan.Active != plan.User {
		return nil
	}
	path := filepath.Join(plan.ArchiveDir, "go-queue-state.json")
	a.queuePath = path
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if a.queueExternal {
			return fmt.Errorf("Local Go 队列存档丢失，已拒绝以空队列启动: %s", path)
		}
		// A v0.10.0 installation stored everything in Roaming/state.json.
		// Preserve the unmodified old snapshot in Local/backups first.
		old, readErr := os.ReadFile(a.dataPath)
		if os.IsNotExist(readErr) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if len(old) > 64<<20 {
			return fmt.Errorf("old Go state is too large to migrate")
		}
		if !json.Valid(old) {
			return fmt.Errorf("old Go state is not valid JSON; migration stopped")
		}
		if err := os.MkdirAll(filepath.Join(plan.BackupDir, "migration-backup"), 0700); err != nil {
			return err
		}
		fingerprint := sha256.Sum256(old)
		backup := filepath.Join(plan.BackupDir, "migration-backup", fmt.Sprintf("go-state-before-split-%x.json", fingerprint[:8]))
		if err := atomicCreateBackup(backup, old); err != nil {
			return err
		}
		a.slots[slotKey(a.config.ArchiveSlot)] = append([]QueueItem{}, a.queue...)
		return a.saveQueueLocalLocked()
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return fmt.Errorf("Go Local 队列存档不是安全的常规文件: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var state localQueue
	if err = json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("Go Local 队列存档损坏: %w", err)
	}
	if state.Schema != 1 || state.Slots == nil {
		return fmt.Errorf("Go Local 队列存档格式不受支持: %s", path)
	}
	a.slots = state.Slots
	a.queue = append([]QueueItem{}, state.Queue...)
	if a.queue == nil {
		a.queue = []QueueItem{}
	}
	if entries, found := a.slots[slotKey(a.config.ArchiveSlot)]; found {
		a.queue = append([]QueueItem{}, entries...)
	}
	return nil
}

func atomicCreateBackup(path string, raw []byte) error {
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		saved, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if !bytes.Equal(saved, raw) {
			return fmt.Errorf("existing migration backup differs: %s", path)
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, err = fd.Write(raw)
	if e := fd.Sync(); err == nil {
		err = e
	}
	if e := fd.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

func atomicLocalWrite(path string, raw []byte) error {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("unsafe queue target: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".queue-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(raw); err != nil {
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
	return os.Rename(f.Name(), path)
}

// Before changing a Go slot, preserve the previous slot content in Python's
// five-column archival form: at most one per half-hour, 96 per slot. Go owns
// only go-* snapshots, and never deletes Python's independent snapshots.
func (a *App) archivePreviousQueue(previous localQueue, next localQueue) error {
	if a.queuePath == "" || a.storagePlan.BackupDir == "" {
		return nil
	}
	for i := 1; i <= 10; i++ {
		slot := slotKey(i)
		old := previous.Slots[slot]
		nextItems := next.Slots[slot]
		if bytes.Equal(mustJSON(old), mustJSON(nextItems)) {
			continue
		}
		if len(old) == 0 {
			continue
		}
		dir := filepath.Join(a.storagePlan.BackupDir, "queue", fmt.Sprintf("queue_archive_slot_%d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		window := time.Now().UTC().Truncate(30 * time.Minute).Format("20060102T1504")
		name := filepath.Join(dir, "go-"+window+".csv")
		if _, err := os.Lstat(name); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		csv, e := encodePythonQueueCSV(old)
		if e != nil {
			return e
		}
		file, e := os.OpenFile(name, os.O_EXCL|os.O_CREATE|os.O_WRONLY, 0600)
		if os.IsExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		_, e = file.Write(csv)
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			os.Remove(name)
			if e != nil {
				return e
			}
			return closeErr
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			return e
		}
		snapshots := []string{}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "go-") && strings.HasSuffix(entry.Name(), ".csv") {
				snapshots = append(snapshots, entry.Name())
			}
		}
		sort.Strings(snapshots)
		for len(snapshots) > 96 {
			_ = os.Remove(filepath.Join(dir, snapshots[0]))
			snapshots = snapshots[1:]
		}
	}
	return nil
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func (a *App) saveQueueLocalLocked() error {
	if a.queuePath == "" {
		return nil
	}
	next := localQueue{Schema: 1, Queue: a.queue, Slots: a.slots}
	raw, e := json.MarshalIndent(next, "", "  ")
	if e != nil {
		return e
	}
	var previous localQueue
	if prior, e := os.ReadFile(a.queuePath); e == nil {
		if e = json.Unmarshal(prior, &previous); e != nil {
			return fmt.Errorf("read previous Go queue: %w", e)
		}
		if e = a.archivePreviousQueue(previous, next); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	return atomicLocalWrite(a.queuePath, raw)
}
