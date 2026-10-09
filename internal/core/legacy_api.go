package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) legacyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/legacy/preview", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		name, raw, e := legacyUpload(w, r)
		if e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.mu.RLock()
		cfg := a.config
		a.mu.RUnlock()
		m, e := parseLegacy(name, raw, cfg)
		if e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		send(w, 200, m.preview)
	})
	mux.HandleFunc("POST /api/legacy/import", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		name, raw, e := legacyUpload(w, r)
		if e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.mu.Lock()
		m, e := parseLegacy(name, raw, a.config)
		if e != nil {
			a.mu.Unlock()
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		dataDir := filepath.Dir(a.dataPath)
		migrationDir := filepath.Join(dataDir, "migration-backup")
		if e = os.MkdirAll(migrationDir, 0700); e != nil {
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		oldRaw, e := json.MarshalIndent(persisted{Config: a.config, Queue: a.queue, Slots: a.slots, Style: a.style, Appearance: a.appearance, DailyPeriod: a.dailyPeriod, DailyCounts: a.dailyCounts}, "", "  ")
		if e == nil {
			e = writePrivate(filepath.Join(migrationDir, "previous-state-"+time.Now().UTC().Format("20060102T150405.000000000")+".json"), oldRaw)
		}
		if e != nil {
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "创建备份失败: " + e.Error()})
			return
		}
		stamp := time.Now().UTC().Format("20060102T150405.000000000")
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".zip" && ext != ".yaml" && ext != ".json" && ext != ".csv" {
			ext = ".dat"
		}
		if e = writePrivate(filepath.Join(migrationDir, "legacy-original-"+stamp+ext), raw); e != nil {
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "保存原始配置失败: " + e.Error()})
			return
		}
		before := persisted{Config: a.config, Queue: a.queue, Slots: a.slots, DailyPeriod: a.dailyPeriod, DailyCounts: a.dailyCounts, GiftCredits: a.giftCredits, GiftUsed: a.giftUsed, GiftSeen: a.giftSeen, Style: a.style, Appearance: a.appearance}
		nextSlots := map[string][]QueueItem{}
		for key, items := range a.slots {
			nextSlots[key] = append([]QueueItem{}, items...)
		}
		a.slots = nextSlots
		a.config = m.cfg
		a.stripBlacklistedRolesLocked()
		if m.dailyPeriod != "" {
			a.dailyPeriod = m.dailyPeriod
			a.dailyCounts = m.dailyCounts
		}
		if len(m.slots) > 0 {
			for key, items := range m.slots {
				a.slots[key] = items
			}
		}
		if m.queue != nil {
			a.queue = m.queue
		}
		if m.style != nil {
			a.style = m.style
		}
		if m.appearance != nil {
			a.appearance = m.appearance
		}
		e = a.saveLocked()
		if e != nil {
			a.config = before.Config
			a.dailyPeriod = before.DailyPeriod
			a.dailyCounts = before.DailyCounts
			a.slots = before.Slots
			a.queue = before.Queue
			a.style = before.Style
			a.appearance = before.Appearance
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "保存失败: " + e.Error()})
			return
		}
		cfg := a.config
		a.publishLocked(Event{Type: "queue", Data: a.queue})
		a.mu.Unlock()
		a.apply(cfg)
		send(w, 200, map[string]any{"status": "ok", "preview": m.preview, "backup_directory": "migration-backup"})
	})
	mux.HandleFunc("GET /api/style", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, a.style)
	})
	mux.HandleFunc("POST /api/style", func(w http.ResponseWriter, r *http.Request) { a.saveMap(w, r, "style") })
	mux.HandleFunc("GET /api/appearance", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, a.appearance)
	})
	mux.HandleFunc("POST /api/appearance", func(w http.ResponseWriter, r *http.Request) { a.saveMap(w, r, "appearance") })
}
func (a *App) saveMap(w http.ResponseWriter, r *http.Request, kind string) {
	if !a.isAdmin(r) {
		send(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	obj := map[string]any{}
	if e := decode(r, &obj); e != nil {
		send(w, 400, map[string]string{"error": e.Error()})
		return
	}
	raw, e := json.Marshal(obj)
	if e != nil || len(raw) > 64000 {
		send(w, 400, map[string]string{"error": "invalid settings size"})
		return
	}
	a.mu.Lock()
	oldStyle, oldAppearance := a.style, a.appearance
	if kind == "style" {
		a.style = obj
	} else {
		a.appearance = obj
	}
	e = a.saveLocked()
	if e == nil {
		e = a.writeWebAppearanceFile(kind, raw)
	}
	if e != nil {
		a.style = oldStyle
		a.appearance = oldAppearance
	}
	a.mu.Unlock()
	if e != nil {
		send(w, 500, map[string]string{"error": e.Error()})
		return
	}
	send(w, 200, map[string]string{"status": "ok"})
}
func legacyUpload(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLegacyBytes+(1<<20))
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		return "", nil, e
	}
	f, h, e := r.FormFile("file")
	if e != nil {
		return "", nil, e
	}
	defer f.Close()
	if h.Size > maxLegacyBytes {
		return "", nil, errors.New("file exceeds 12 MiB")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxLegacyBytes+1))
	if e != nil {
		return "", nil, e
	}
	if len(b) > maxLegacyBytes {
		return "", nil, errors.New("file exceeds 12 MiB")
	}
	return filepath.Base(h.Filename), b, nil
}
func writePrivate(name string, data []byte) error {
	f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write private file: %w", e)
	}
	return nil
}
