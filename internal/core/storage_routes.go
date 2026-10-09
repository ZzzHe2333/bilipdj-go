package core

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
)

func (a *App) storageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/storage/status", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		p := a.storagePlan
		if p.Mode == "" {
			p = storage.Plan{Mode: "explicit", Choice: "explicit", Active: filepath.Dir(a.dataPath), Legacy: filepath.Dir(a.dataPath), User: filepath.Dir(a.dataPath)}
		}
		send(w, 200, p)
	})
	mux.HandleFunc("POST /api/storage/choice", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var payload struct {
			Choice string `json:"choice"`
		}
		if e := decode(r, &payload); e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		if e := storage.Choose(a.storagePlan, payload.Choice); e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		send(w, 200, map[string]any{"status": "ok", "choice": payload.Choice, "restart_required": true})
	})
	// Python archives stay read-only. A deliberate import writes Go's state.json
	// and takes a separate snapshot before modifying any queue slots.
	mux.HandleFunc("GET /api/storage/python-queues", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		counts, _, err := readPythonQueueSlots(filepath.Dir(a.dataPath))
		if err != nil {
			send(w, 400, map[string]string{"error": err.Error()})
			return
		}
		send(w, 200, map[string]any{"counts": counts, "available": len(counts) > 0, "directory": "core/cd", "read_only": true})
	})
	mux.HandleFunc("POST /api/storage/python-queues/import", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		counts, slots, err := readPythonQueueSlots(filepath.Dir(a.dataPath))
		if err != nil {
			send(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if len(slots) == 0 {
			send(w, 404, map[string]string{"error": "未发现 Python core/cd 存档"})
			return
		}
		a.mu.Lock()
		previous, err := os.ReadFile(a.dataPath)
		if os.IsNotExist(err) {
			err = nil
		}
		if err != nil {
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "无法备份原 Go 存档"})
			return
		}
		if len(previous) > 0 {
			dir := filepath.Join(filepath.Dir(a.dataPath), "migration-backup")
			if err = os.MkdirAll(dir, 0700); err == nil {
				name := filepath.Join(dir, "previous-go-state-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
				err = os.WriteFile(name, previous, 0600)
			}
		}
		if err != nil {
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "存档备份失败: " + err.Error()})
			return
		}
		beforeSlots, beforeQueue := a.slots, a.queue
		next := make(map[string][]QueueItem, len(a.slots))
		for k, v := range a.slots {
			next[k] = append([]QueueItem{}, v...)
		}
		for k, v := range slots {
			next[k] = append([]QueueItem{}, v...)
		}
		a.slots = next
		if v, ok := slots[slotKey(a.config.ArchiveSlot)]; ok {
			a.queue = append([]QueueItem{}, v...)
		}
		if err = a.saveLocked(); err != nil {
			a.slots, a.queue = beforeSlots, beforeQueue
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "Go 状态写入失败: " + err.Error()})
			return
		}
		a.publishLocked(Event{Type: "queue", Data: a.queue})
		a.mu.Unlock()
		send(w, 200, map[string]any{"status": "ok", "slot_counts": counts, "message": "已导入到 Go state.json；Python 原始 CSV 未修改"})
	})
	mux.HandleFunc("GET /api/storage/queue-export", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		num, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil || num < 1 || num > 10 {
			send(w, 400, map[string]string{"error": "slot must be 1-10"})
			return
		}
		a.mu.RLock()
		queue := append([]QueueItem{}, a.slots[slotKey(num)]...)
		if num == a.config.ArchiveSlot {
			queue = append([]QueueItem{}, a.queue...)
		}
		a.mu.RUnlock()
		raw, err := encodePythonQueueCSV(queue)
		if err != nil {
			send(w, 500, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="queue_archive_slot_%d.csv"`, num))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(raw)
	})
}
func readPythonQueueSlots(dir string) (map[string]int, map[string][]QueueItem, error) {
	counts := map[string]int{}
	slots := map[string][]QueueItem{}
	base := filepath.Join(dir, "core", "cd")
	info, err := os.Lstat(base)
	if os.IsNotExist(err) {
		return counts, slots, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("core/cd 不是目录")
	}
	for i := 1; i <= 10; i++ {
		name := filepath.Join(base, fmt.Sprintf("queue_archive_slot_%d.csv", i))
		st, e := os.Lstat(name)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, nil, e
		}
		if !st.Mode().IsRegular() || st.Size() > 12<<20 {
			return nil, nil, fmt.Errorf("invalid Python archive %d", i)
		}
		raw, e := os.ReadFile(name)
		if e != nil {
			return nil, nil, e
		}
		entries, e := queueCSV(raw)
		if e != nil {
			return nil, nil, e
		}
		key := slotKey(i)
		slots[key] = entries
		counts[key] = len(entries)
	}
	return counts, slots, nil
}
func encodePythonQueueCSV(queue []QueueItem) ([]byte, error) {
	var out bytes.Buffer
	out.Write([]byte{0xef, 0xbb, 0xbf})
	cw := csv.NewWriter(&out)
	now := time.Now().Format("2006-01-02T15:04:05")
	for _, row := range [][]string{{"最后操作时间", now}, {"操作人", "bilipdj-go"}, {"操作说明", "explicit_export"}, {}, {"序号", "id", "内容", "最后操作时间", "来源平台"}} {
		if e := cw.Write(row); e != nil {
			return nil, e
		}
	}
	for i, q := range queue {
		platform := strings.ToLower(q.Platform)
		if platform == "legacy" {
			platform = ""
		}
		at := now
		if !q.At.IsZero() {
			at = q.At.Format("2006-01-02T15:04:05")
		}
		if e := cw.Write([]string{strconv.Itoa(i + 1), q.Username, q.Note, at, platform}); e != nil {
			return nil, e
		}
	}
	cw.Flush()
	return out.Bytes(), cw.Error()
}
