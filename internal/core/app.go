package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"github.com/ZzzHe2333/bilipdj-go/internal/update"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type PlatformConfig struct {
	Enabled bool   `json:"enabled"`
	Room    string `json:"room"`
	Cookie  string `json:"cookie,omitempty"`
}
type Config struct {
	Bilibili    PlatformConfig `json:"bilibili"`
	Douyin      PlatformConfig `json:"douyin"`
	AutoQueue   bool           `json:"auto_queue"`
	Command     string         `json:"command"`
	MaxQueue    int            `json:"max_queue"`
	Admins      []string       `json:"admins"`
	Blacklist   []string       `json:"blacklist"`
	Language    string         `json:"language"`
	ArchiveSlot int            `json:"archive_slot"`
	Switches    *QueueSwitches `json:"switches,omitempty"`
}

// QueueSwitches maps the supported legacy kaiguan.yaml flags.
// The pointer differentiates an old saved state from disabled settings.
type QueueSwitches struct {
	Paidui bool `json:"paidui"`
	Guanfu bool `json:"guanfu_paidui"`
	Bfu    bool `json:"bfu_paidui"`
	Chaoji bool `json:"chaoji_paidui"`
	Mifu   bool `json:"mifu_paidui"`
	Cancel bool `json:"quxiao_paidui"`
	Modify bool `json:"xiugai_paidui"`
}

func defaultSwitches() *QueueSwitches {
	return &QueueSwitches{Paidui: true, Guanfu: true, Bfu: true, Chaoji: true, Mifu: true, Cancel: true, Modify: true}
}

type QueueItem struct {
	Mode     string    `json:"mode,omitempty"`
	Key      string    `json:"key"`
	Platform string    `json:"platform"`
	UserID   string    `json:"user_id"`
	Username string    `json:"username"`
	Note     string    `json:"note"`
	At       time.Time `json:"at"`
}
type persisted struct {
	Config     Config                 `json:"config"`
	Queue      []QueueItem            `json:"queue"`
	Slots      map[string][]QueueItem `json:"slots,omitempty"`
	Style      map[string]any         `json:"style,omitempty"`
	Appearance map[string]any         `json:"appearance,omitempty"`
}
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
type App struct {
	mu          sync.RWMutex
	config      Config
	queue       []QueueItem
	slots       map[string][]QueueItem
	style       map[string]any
	appearance  map[string]any
	messages    []live.Event
	statuses    map[string]live.Status
	subscribers map[chan Event]struct{}
	workers     map[string]context.CancelFunc
	dataPath    string
	version     string
	repo        string
	updater     *update.Service
}

func New(dataDir, version, repo string) *App {
	a := &App{config: defaultConfig(), style: defaultStyle(), appearance: defaultAppearance(), queue: []QueueItem{}, slots: map[string][]QueueItem{}, messages: []live.Event{}, statuses: map[string]live.Status{}, subscribers: map[chan Event]struct{}{}, workers: map[string]context.CancelFunc{}, dataPath: filepath.Join(dataDir, "state.json"), version: version, repo: repo}
	a.updater = update.New(repo, version, dataDir)
	raw, e := os.ReadFile(a.dataPath)
	if e == nil {
		var p persisted
		if json.Unmarshal(raw, &p) == nil {
			a.config = p.Config
			if a.config.Switches == nil {
				a.config.Switches = defaultSwitches()
			}
			if a.config.MaxQueue == 0 {
				a.config.MaxQueue = 100
			}
			if a.config.Language == "" {
				a.config.Language = "中文"
			}
			if a.config.ArchiveSlot == 0 {
				a.config.ArchiveSlot = 1
			}
			if p.Slots != nil {
				for n, entries := range p.Slots {
					a.slots[n] = append([]QueueItem{}, entries...)
				}
			}
			a.queue = p.Queue
			if len(a.slots) > 0 {
				a.queue = append([]QueueItem{}, a.slots[slotKey(a.config.ArchiveSlot)]...)
			}
			if p.Style != nil {
				a.style = p.Style
			}
			if p.Appearance != nil {
				a.appearance = p.Appearance
			}
			if a.queue == nil {
				a.queue = []QueueItem{}
			}
			if a.config.Command == "" {
				a.config.Command = "排队"
			}
		}
	}
	return a
}
func (a *App) saveLocked() error {
	a.slots[slotKey(a.config.ArchiveSlot)] = append([]QueueItem{}, a.queue...)
	raw, e := json.MarshalIndent(persisted{Config: a.config, Queue: a.queue, Slots: a.slots, Style: a.style, Appearance: a.appearance}, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(a.dataPath), 0700); e != nil {
		return e
	}
	file, e := os.CreateTemp(filepath.Dir(a.dataPath), ".state-*")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	if e = file.Chmod(0600); e != nil {
		file.Close()
		return e
	}
	if _, e = file.Write(raw); e != nil {
		file.Close()
		return e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	return os.Rename(name, a.dataPath)
}
func slotKey(n int) string {
	if n < 1 {
		n = 1
	}
	return fmt.Sprint(n)
}
func (a *App) switchSlotLocked(n int) {
	if n == a.config.ArchiveSlot {
		return
	}
	a.slots[slotKey(a.config.ArchiveSlot)] = append([]QueueItem{}, a.queue...)
	a.queue = append([]QueueItem{}, a.slots[slotKey(n)]...)
	a.config.ArchiveSlot = n
}
func (a *App) slotCountsLocked() map[string]int {
	counts := map[string]int{}
	for i := 1; i <= 10; i++ {
		counts[slotKey(i)] = len(a.slots[slotKey(i)])
	}
	counts[slotKey(a.config.ArchiveSlot)] = len(a.queue)
	return counts
}
func (a *App) publishLocked(e Event) {
	for ch := range a.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}
func (a *App) publishStatus(s live.Status) {
	a.mu.Lock()
	a.statuses[s.Platform] = s
	a.publishLocked(Event{Type: "status", Data: s})
	a.mu.Unlock()
}
func (a *App) OnDanmu(e live.Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Username = strings.TrimSpace(e.Username)
	e.Content = strings.TrimSpace(e.Content)
	if e.Username == "" || e.Content == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.messages = append(a.messages, e)
	if len(a.messages) > 150 {
		a.messages = append([]live.Event{}, a.messages[len(a.messages)-150:]...)
	}
	a.publishLocked(Event{Type: "danmu", Data: e})
	for _, s := range a.config.Blacklist {
		if strings.EqualFold(strings.TrimSpace(s), e.Username) || s == e.UserID {
			return
		}
	}
	if a.processDanmuCommandLocked(e) {
		if err := a.saveLocked(); err != nil {
			log.Printf("queue persist: %v", err)
		}
		a.publishLocked(Event{Type: "queue", Data: a.queue})
	}

}
func (a *App) Start() { a.mu.RLock(); c := a.config; a.mu.RUnlock(); a.apply(c) }
func (a *App) apply(cfg Config) {
	a.mu.Lock()
	for _, cancel := range a.workers {
		cancel()
	}
	a.workers = map[string]context.CancelFunc{}
	for _, platform := range []string{"bilibili", "douyin"} {
		var pc PlatformConfig
		var source live.Source
		if platform == "bilibili" {
			pc = cfg.Bilibili
			source = live.Bilibili{}
		} else {
			pc = cfg.Douyin
			source = live.Douyin{}
		}
		if pc.Enabled && strings.TrimSpace(pc.Room) != "" {
			ctx, cancel := context.WithCancel(context.Background())
			a.workers[platform] = cancel
			go source.Run(ctx, pc.Room, pc.Cookie, a.OnDanmu, a.publishStatus)
		} else {
			a.statuses[platform] = live.Status{Platform: platform, Connected: false, Message: "未启用", Since: time.Now()}
		}
	}
	a.mu.Unlock()
}
func (a *App) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, stop := range a.workers {
		stop()
	}
	a.workers = map[string]context.CancelFunc{}
}
func send(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 16<<10)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(dest); e != nil {
		return e
	}
	return nil
}
func cleanConfig(c Config) (Config, error) {
	c.Bilibili.Room = strings.TrimSpace(c.Bilibili.Room)
	c.Douyin.Room = strings.TrimSpace(c.Douyin.Room)
	c.Command = strings.TrimSpace(c.Command)
	if c.Command == "" {
		c.Command = "排队"
	}
	if len([]rune(c.Command)) > 16 {
		return c, errors.New("口令最多 16 个字符")
	}
	if len(c.Bilibili.Cookie) > 8192 || len(c.Douyin.Cookie) > 8192 {
		return c, errors.New("Cookie 超过长度限制")
	}
	if c.MaxQueue < 0 || c.MaxQueue > 10000 {
		return c, errors.New("排队人数上限必须为 0 至 10000")
	}
	if c.ArchiveSlot < 0 || c.ArchiveSlot > 10 {
		return c, errors.New("存档槽位必须为 1 至 10")
	}
	if c.ArchiveSlot == 0 {
		c.ArchiveSlot = 1
	}
	if c.Switches == nil {
		c.Switches = defaultSwitches()
	}
	if len(c.Blacklist) > 5000 || len(c.Admins) > 1000 {
		return c, errors.New("黑名单或管理员数量超过上限")
	}
	if c.Language == "" {
		c.Language = "中文"
	}
	if c.Bilibili.Enabled {
		if _, e := live.ValidateBilibiliRoom(c.Bilibili.Room); e != nil {
			return c, e
		}
	}
	if c.Douyin.Enabled {
		if _, e := live.ValidateDouyinRoom(c.Douyin.Room); e != nil {
			return c, e
		}
	}
	return c, nil
}
func (a *App) isAdmin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, e := url.Parse(origin)
		if e != nil || u.Host != r.Host || !strings.HasPrefix(u.Scheme, "http") {
			return false
		}
	}
	token := os.Getenv("BILIPDJ_ADMIN_TOKEN")
	if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Token")), []byte(token)) == 1 {
		return true
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	hostname := strings.Split(r.Host, ":")[0]
	return ip != nil && ip.IsLoopback() && (hostname == "127.0.0.1" || hostname == "localhost" || hostname == "[::1]")
}
func (a *App) Routes(ui http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		send(w, 200, map[string]any{"status": "ok", "version": a.version})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, map[string]any{"version": a.version, "platforms": a.statuses, "queue_size": len(a.queue)})
	})
	mux.HandleFunc("GET /api/runtime-status", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, map[string]any{"version": a.version, "platforms": a.statuses, "queue_size": len(a.queue)})
	})
	mux.HandleFunc("GET /api/queue/state", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, map[string]any{"entries": a.queue, "count": len(a.queue), "active_slot": a.config.ArchiveSlot})
	})
	mux.HandleFunc("GET /api/queue/slots", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, map[string]any{"active_slot": a.config.ArchiveSlot, "slots": a.slotCountsLocked()})
	})
	mux.HandleFunc("POST /api/queue/slots", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var payload struct {
			Slot int `json:"slot"`
		}
		if e := decode(r, &payload); e != nil || payload.Slot < 1 || payload.Slot > 10 {
			send(w, 400, map[string]string{"error": "存档槽位必须为 1 至 10"})
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.switchSlotLocked(payload.Slot)
		if e := a.saveLocked(); e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.publishLocked(Event{Type: "queue", Data: a.queue})
		send(w, 200, map[string]any{"active_slot": a.config.ArchiveSlot, "slots": a.slotCountsLocked(), "entries": a.queue})
	})
	mux.HandleFunc("GET /api/platforms/active", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		active := []string{}
		if a.config.Bilibili.Enabled {
			active = append(active, "bilibili")
		}
		if a.config.Douyin.Enabled {
			active = append(active, "douyin")
		}
		send(w, 200, map[string]any{"active": active})
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		c := a.config
		c.Bilibili.Cookie = ""
		c.Douyin.Cookie = ""
		send(w, 200, map[string]any{"config": c, "cookie_configured": map[string]bool{"bilibili": a.config.Bilibili.Cookie != "", "douyin": a.config.Douyin.Cookie != ""}})
	})
	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "仅本机或管理员 Token 可写"})
			return
		}
		var cfg Config
		if e := decode(r, &cfg); e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.mu.Lock()
		if cfg.Bilibili.Cookie == "" {
			cfg.Bilibili.Cookie = a.config.Bilibili.Cookie
		}
		if cfg.Douyin.Cookie == "" {
			cfg.Douyin.Cookie = a.config.Douyin.Cookie
		}
		a.mu.Unlock()
		cfg, e := cleanConfig(cfg)
		if e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.mu.Lock()
		if cfg.ArchiveSlot != a.config.ArchiveSlot {
			a.switchSlotLocked(cfg.ArchiveSlot)
		}
		a.config = cfg
		e = a.saveLocked()
		a.mu.Unlock()
		if e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.apply(cfg)
		send(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/queue", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, a.queue)
	})
	mux.HandleFunc("POST /api/queue", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var req struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Name   string `json:"name"`
			Note   string `json:"note"`
			Index  int    `json:"index"`
		}
		if e := decode(r, &req); e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		switch req.Action {
		case "clear":
			a.queue = []QueueItem{}
		case "remove":
			items := a.queue[:0]
			for _, q := range a.queue {
				if q.Key != req.Key {
					items = append(items, q)
				}
			}
			a.queue = items
		case "add":
			name := strings.TrimSpace(req.Name)
			if name == "" || len([]rune(name)) > 60 {
				send(w, 400, map[string]string{"error": "用户名无效"})
				return
			}
			if a.config.MaxQueue > 0 && len(a.queue) >= a.config.MaxQueue {
				send(w, 409, map[string]string{"error": "排队人数已达上限"})
				return
			}
			key := fmt.Sprintf("manual:%d", time.Now().UnixNano())
			a.queue = append(a.queue, QueueItem{Key: key, Platform: "manual", Username: name, Note: strings.TrimSpace(req.Note), At: time.Now()})
		case "move":
			idx := -1
			for i, q := range a.queue {
				if q.Key == req.Key {
					idx = i
					break
				}
			}
			if idx < 0 || req.Index < 0 || req.Index >= len(a.queue) {
				send(w, 400, map[string]string{"error": "invalid queue position"})
				return
			}
			q := a.queue[idx]
			a.queue = append(a.queue[:idx], a.queue[idx+1:]...)
			a.queue = append(a.queue, QueueItem{})
			copy(a.queue[req.Index+1:], a.queue[req.Index:])
			a.queue[req.Index] = q
		case "edit":
			found := false
			for i := range a.queue {
				if a.queue[i].Key == req.Key {
					a.queue[i].Note = strings.TrimSpace(req.Note)
					found = true
					break
				}
			}
			if !found {
				send(w, 404, map[string]string{"error": "queue item not found"})
				return
			}
		default:
			send(w, 400, map[string]string{"error": "unknown action"})
			return
		}
		if e := a.saveLocked(); e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.publishLocked(Event{Type: "queue", Data: a.queue})
		send(w, 200, a.queue)
	})
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, a.messages)
	})
	mux.HandleFunc("GET /api/events", a.events)
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		v, e := a.updater.Check(ctx)
		if e != nil {
			send(w, 502, map[string]string{"error": e.Error()})
			return
		}
		send(w, 200, v)
	})
	mux.HandleFunc("POST /api/update/download", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		result, e := a.updater.Download(ctx)
		if e != nil {
			send(w, 502, map[string]string{"error": e.Error()})
			return
		}
		send(w, 200, result)
	})
	a.legacyRoutes(mux)
	mux.HandleFunc("GET /control", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusTemporaryRedirect) })
	mux.HandleFunc("GET /index", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/overlay.html", http.StatusTemporaryRedirect)
	})
	mux.Handle("/", ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan Event, 64)
	a.mu.Lock()
	a.subscribers[ch] = struct{}{}
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.subscribers, ch); a.mu.Unlock() }()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-t.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
