package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"github.com/ZzzHe2333/bilipdj-go/internal/perf"
	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
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
	Bilibili            PlatformConfig `json:"bilibili"`
	Douyin              PlatformConfig `json:"douyin"`
	Huya                PlatformConfig `json:"huya"`
	WechatMP            PlatformConfig `json:"wechat_mp"`
	Kuaishou            PlatformConfig `json:"kuaishou"`
	Douyu               PlatformConfig `json:"douyu"`
	VisiblePlatforms    []string       `json:"visible_platforms,omitempty"`
	AutoQueue           bool           `json:"auto_queue"`
	Command             string         `json:"command"`
	MaxQueue            int            `json:"max_queue"`
	Admins              []string       `json:"admins"`
	SuperAdmins         []string       `json:"super_admins"`
	Guards              []string       `json:"guards"`
	DailyQueueLimit     int            `json:"daily_queue_limit"`
	DailyQueueResetTime string         `json:"daily_queue_reset_time"`
	Blacklist           []string       `json:"blacklist"`
	Language            string         `json:"language"`
	ArchiveSlot         int            `json:"archive_slot"`
	GiftQueue           *GiftSettings  `json:"gift_queue,omitempty"`
	Switches            *QueueSwitches `json:"switches,omitempty"`
}

// QueueSwitches maps the supported legacy kaiguan.yaml flags.
// The pointer differentiates an old saved state from disabled settings.
type QueueSwitches struct {
	Paidui            bool `json:"paidui"`
	Guanfu            bool `json:"guanfu_paidui"`
	Bfu               bool `json:"bfu_paidui"`
	Chaoji            bool `json:"chaoji_paidui"`
	Mifu              bool `json:"mifu_paidui"`
	Cancel            bool `json:"quxiao_paidui"`
	Modify            bool `json:"xiugai_paidui"`
	GuardInsert       bool `json:"jianzhang_chadui"`
	RoomAdminOperator bool `json:"fangguan_op"`
}

func defaultSwitches() *QueueSwitches {
	return &QueueSwitches{Paidui: true, Guanfu: true, Bfu: true, Chaoji: true, Mifu: true, Cancel: true, Modify: true, GuardInsert: false, RoomAdminOperator: false}
}

// GiftSettings follows the supported original myjs gift queue settings.
// Battery eligibility uses a local verified price catalog, not untrusted prices.
type GiftSettings struct {
	Enabled       bool     `json:"enabled"`
	Names         []string `json:"names"`
	MinBatteries  int      `json:"min_batteries"`
	AllowMultiple bool     `json:"allow_multiple"`
	SlotsPerGift  int      `json:"slots_per_gift"`
	InsertRank    int      `json:"insert_rank"`
	GiftOnly      bool     `json:"gift_only"`
}

func defaultGiftSettings() *GiftSettings {
	return &GiftSettings{Names: []string{}, SlotsPerGift: 1, InsertRank: 1}
}

type QueueItem struct {
	Mode     string    `json:"mode,omitempty"`
	Key      string    `json:"key"`
	Platform string    `json:"platform"`
	// SourcePlatform is a human-entered display label on manually queued entries.
	// Platform, UserID and Key remain the trusted origin/identity for live events.
	SourcePlatform string `json:"source_platform,omitempty"`
	UserID   string    `json:"user_id"`
	Username string    `json:"username"`
	Note     string    `json:"note"`
	At       time.Time `json:"at"`
	Guard    bool      `json:"guard,omitempty"`
}
type persisted struct {
	OnboardingCompleted bool                   `json:"onboarding_completed"`
	Config              Config                 `json:"config"`
	Queue               []QueueItem            `json:"queue,omitempty"`
	QueueExternal       bool                   `json:"queue_external,omitempty"`
	Slots               map[string][]QueueItem `json:"slots,omitempty"`
	DailyPeriod         string                 `json:"daily_period,omitempty"`
	DailyCounts         map[string]int         `json:"daily_counts,omitempty"`
	GiftCredits         map[string]int         `json:"gift_credits,omitempty"`
	GiftUsed            map[string]bool        `json:"gift_used,omitempty"`
	GiftSeen            []string               `json:"gift_seen,omitempty"`
	Style               map[string]any         `json:"style,omitempty"`
	Appearance          map[string]any         `json:"appearance,omitempty"`
}
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
type App struct {
	mu                  sync.RWMutex
	config              Config
	queue               []QueueItem
	slots               map[string][]QueueItem
	dailyPeriod         string
	dailyCounts         map[string]int
	giftCredits         map[string]int
	giftUsed            map[string]bool
	giftSeen            []string
	giftLast            *live.Event
	style               map[string]any
	appearance          map[string]any
	messages            []live.Event
	logs                []LogEntry
	logSequence         uint64
	statuses            map[string]live.Status
	subscribers         map[chan Event]struct{}
	workers             map[string]context.CancelFunc
	dataPath            string
	queuePath           string
	queueExternal       bool
	version             string
	onboardingCompleted bool
	repo                string
	updater             *update.Service
	updateMu            sync.Mutex
	progressMu          sync.RWMutex
	updateProgress      update.DownloadProgress
	performance         *perf.Monitor
	preparedUpdate      update.Downloaded
	installAction       func(update.Downloaded) error
	qrMu                sync.Mutex
	qrSession           biliQRSession
	qrClient            *http.Client
	qrGenerateURL       string
	qrPollURL           string
	qrNavURL            string
	storagePlan         storage.Plan
}

func (a *App) SetInstallAction(f func(update.Downloaded) error) { a.installAction = f }

func New(dataDir, version, repo string) *App {
	a := &App{config: defaultConfig(), style: defaultStyle(), appearance: defaultAppearance(), queue: []QueueItem{}, slots: map[string][]QueueItem{}, dailyCounts: map[string]int{}, giftCredits: map[string]int{}, giftUsed: map[string]bool{}, messages: []live.Event{}, statuses: map[string]live.Status{}, subscribers: map[chan Event]struct{}{}, workers: map[string]context.CancelFunc{}, dataPath: filepath.Join(dataDir, "state.json"), version: version, repo: repo}
	a.updater = update.New(repo, version, dataDir)
	a.performance = perf.New(dataDir)
	a.qrGenerateURL = biliQRGenerateEndpoint
	a.qrPollURL = biliQRPollEndpoint
	a.qrNavURL = biliNavEndpoint
	a.appendLogLocked("INFO", "system", "BiliPDJ Go 已启动，正在读取本地配置")
	raw, e := os.ReadFile(a.dataPath)
	if e == nil {
		var p persisted
		if json.Unmarshal(raw, &p) == nil {
			// Existing installations predate the wizard. Do not interrupt their
			// configured workflow on upgrade; they can reopen it from the sidebar.
			// New installs (no state.json) show the wizard exactly once.
			var keys map[string]json.RawMessage
			_ = json.Unmarshal(raw, &keys)
			_, hasWizardField := keys["onboarding_completed"]
			a.onboardingCompleted = p.OnboardingCompleted || !hasWizardField
			a.queueExternal = p.QueueExternal
			a.config = p.Config
			a.dailyPeriod = p.DailyPeriod
			if p.DailyCounts != nil {
				a.dailyCounts = p.DailyCounts
			}
			if p.GiftCredits != nil {
				a.giftCredits = p.GiftCredits
			}
			if p.GiftUsed != nil {
				a.giftUsed = p.GiftUsed
			}
			a.giftSeen = p.GiftSeen
			if a.config.GiftQueue == nil {
				a.config.GiftQueue = defaultGiftSettings()
			}
			if a.config.Switches == nil {
				a.config.Switches = defaultSwitches()
			}
			if a.config.MaxQueue == 0 {
				a.config.MaxQueue = 100
			}
			if a.config.DailyQueueResetTime == "" {
				a.config.DailyQueueResetTime = "04:00"
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
				for key, value := range p.Style { a.style[key] = value }
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
func (a *App) SetStoragePlan(plan storage.Plan) error {
	a.storagePlan = plan
	if plan.Mode == "managed" && plan.Active == plan.User && plan.CacheDir != "" {
		a.updater.Dir = plan.CacheDir
	}
	if err := a.configureLocalQueue(plan); err != nil {
		return err
	}
	a.loadWebAppearanceFiles()
	return nil
}

func (a *App) saveLocked() error {
	a.slots[slotKey(a.config.ArchiveSlot)] = append([]QueueItem{}, a.queue...)
	if err := a.saveQueueLocalLocked(); err != nil {
		return err
	}
	savedQueue, savedSlots := a.queue, a.slots
	if a.queuePath != "" {
		savedQueue = nil
		savedSlots = nil
	}
	raw, e := json.MarshalIndent(persisted{OnboardingCompleted: a.onboardingCompleted, Config: a.config, Queue: savedQueue, QueueExternal: a.queuePath != "", Slots: savedSlots, DailyPeriod: a.dailyPeriod, DailyCounts: a.dailyCounts, GiftCredits: a.giftCredits, GiftUsed: a.giftUsed, GiftSeen: a.giftSeen, Style: a.style, Appearance: a.appearance}, "", "  ")
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
	// Queue events outlive the lock: SSE consumers marshal them after this
	// function returns while another goroutine may edit a.queue in place.
	// Keep an immutable snapshot in each queued event to prevent data races
	// and reporting a different queue than the one that was published.
	if e.Type == "queue" {
		if entries, ok := e.Data.([]QueueItem); ok {
			e.Data = append([]QueueItem{}, entries...)
		}
	}
	for ch := range a.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}
func (a *App) publishStatus(s live.Status) {
	a.mu.Lock()
	previous, exists := a.statuses[s.Platform]
	a.statuses[s.Platform] = s
	if !exists || previous.Message != s.Message || previous.Connected != s.Connected {
		level := "INFO"
		if !s.Connected && (strings.Contains(s.Message, "失败") || strings.Contains(s.Message, "中断") || strings.Contains(s.Message, "错误")) {
			level = "ERROR"
		}
		a.appendLogLocked(level, s.Platform, s.Message)
	}
	a.publishLocked(Event{Type: "status", Data: s})
	a.mu.Unlock()
}
func (a *App) OnDanmu(e live.Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Username = strings.TrimSpace(e.Username)
	e.Content = strings.TrimSpace(e.Content)
	if e.Username == "" || (e.Content == "" && e.Kind != "gift") {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.Kind == "gift" && e.Platform == "bilibili" && e.Gift != nil {
		a.processGiftLocked(e)
		return
	}
	a.messages = append(a.messages, e)
	if len(a.messages) > 150 {
		a.messages = append([]live.Event{}, a.messages[len(a.messages)-150:]...)
	}
	a.publishLocked(Event{Type: "danmu", Data: e})
	a.appendLogLocked("INFO", e.Platform, e.Username+"："+e.Content)
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
		a.appendLogLocked("INFO", "queue", fmt.Sprintf("%s 排队操作已更新队列，当前 %d 人（槽位 %d）", e.Username, len(a.queue), a.config.ArchiveSlot))
	}

}
func (a *App) Start() {
	a.mu.RLock()
	c := a.config
	a.mu.RUnlock()
	a.apply(c)
	a.logEvent("INFO", "system", "弹幕监听服务已启动")
}
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
			a.appendLogLocked("INFO", platform, "监听未启用")
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
	c.Huya.Room = strings.TrimSpace(c.Huya.Room)
	c.WechatMP.Room = strings.TrimSpace(c.WechatMP.Room)
	c.Kuaishou.Room = strings.TrimSpace(c.Kuaishou.Room)
	c.Douyu.Room = strings.TrimSpace(c.Douyu.Room)
	// Placeholder channels may store future room settings, but must never claim to be monitored.
	if c.Huya.Enabled || c.WechatMP.Enabled || c.Kuaishou.Enabled || c.Douyu.Enabled {
		return c, errors.New("虎牙、微信公众号、快手及斗鱼暂未接入弹幕监听，请等待后续版本")
	}
	allowed := map[string]bool{"bilibili": true, "douyin": true, "huya": true, "wechat_mp": true, "kuaishou": true, "douyu": true}
	seen := map[string]bool{}
	visible := make([]string, 0, len(c.VisiblePlatforms))
	for _, id := range c.VisiblePlatforms {
		if !allowed[id] { return c, errors.New("不支持的平台配置项: " + id) }
		if !seen[id] { visible = append(visible, id); seen[id] = true }
	}
	c.VisiblePlatforms = visible
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
	if c.DailyQueueLimit < 0 || c.DailyQueueLimit > 999 {
		return c, errors.New("每日排队次数上限必须为 0 至 999")
	}
	if c.DailyQueueResetTime == "" {
		c.DailyQueueResetTime = "04:00"
	}
	if _, e := time.Parse("15:04", c.DailyQueueResetTime); e != nil || len(c.DailyQueueResetTime) != 5 {
		return c, errors.New("每日重置时间必须为 HH:MM")
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
	if c.GiftQueue == nil {
		c.GiftQueue = defaultGiftSettings()
	}
	if c.GiftQueue.MinBatteries < 0 || c.GiftQueue.MinBatteries > 10000000 || c.GiftQueue.SlotsPerGift < 1 || c.GiftQueue.SlotsPerGift > 100 || c.GiftQueue.InsertRank < 0 || c.GiftQueue.InsertRank > 10000 || len(c.GiftQueue.Names) > 100 {
		return c, errors.New("礼物资格设置超出范围")
	}
	for _, name := range c.GiftQueue.Names {
		if len([]rune(name)) > 80 {
			return c, errors.New("礼物名过长")
		}
	}
	if len(c.Blacklist) > 5000 || len(c.Admins) > 1000 || len(c.SuperAdmins) > 1000 || len(c.Guards) > 1000 {
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
	// Only the active performance page polls this endpoint; there is no
	// background sampling when the user switches pages or selects interval 0.
	mux.HandleFunc("GET /api/performance", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		send(w, 200, a.performance.Sample())
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		a.mu.RLock()
		logs := append([]LogEntry{}, a.logs...)
		a.mu.RUnlock()
		send(w, 200, logs)
	})
	mux.HandleFunc("GET /api/onboarding", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		a.mu.RLock()
		completed := a.onboardingCompleted
		a.mu.RUnlock()
		send(w, 200, map[string]bool{"completed": completed})
	})
	mux.HandleFunc("POST /api/onboarding", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var req struct {
			Completed bool `json:"completed"`
		}
		if err := decode(r, &req); err != nil || !req.Completed {
			send(w, 400, map[string]string{"error": "仅支持完成或跳过引导"})
			return
		}
		a.mu.Lock()
		before := a.onboardingCompleted
		a.onboardingCompleted = true
		if err := a.saveLocked(); err != nil {
			a.onboardingCompleted = before
			a.mu.Unlock()
			send(w, 500, map[string]string{"error": "保存首次使用状态失败"})
			return
		}
		a.appendLogLocked("INFO", "system", "首次使用引导已完成或跳过")
		a.mu.Unlock()
		send(w, 200, map[string]bool{"completed": true})
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
	mux.HandleFunc("GET /api/gifts/state", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, map[string]any{"settings": a.config.GiftQueue, "active_credits": len(a.giftCredits), "used_users": len(a.giftUsed), "last_event": a.giftLast})
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
		a.appendLogLocked("INFO", "queue", fmt.Sprintf("切换到队列存档 %d，现有 %d 人", a.config.ArchiveSlot, len(a.queue)))
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
		c.Huya.Cookie = ""
		c.WechatMP.Cookie = ""
		c.Kuaishou.Cookie = ""
		c.Douyu.Cookie = ""
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
		if cfg.Huya.Cookie == "" { cfg.Huya.Cookie = a.config.Huya.Cookie }
		if cfg.WechatMP.Cookie == "" { cfg.WechatMP.Cookie = a.config.WechatMP.Cookie }
		if cfg.Kuaishou.Cookie == "" { cfg.Kuaishou.Cookie = a.config.Kuaishou.Cookie }
		if cfg.Douyu.Cookie == "" { cfg.Douyu.Cookie = a.config.Douyu.Cookie }
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
		// A blacklisted name cannot retain operator/guard roles.
		a.stripBlacklistedRolesLocked()
		e = a.saveLocked()
		a.mu.Unlock()
		if e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.apply(cfg)
		a.logEvent("INFO", "system", "配置已保存，平台监听已更新")
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
			NameEdit *string `json:"new_name,omitempty"`
			SourcePlatform *string `json:"source_platform,omitempty"`
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
		case "add", "insert":
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
			item := QueueItem{Key: key, Platform: "manual", Username: name, Note: strings.TrimSpace(req.Note), At: time.Now()}
			if req.Action == "insert" {
				if req.Index < 0 || req.Index > len(a.queue) {
					send(w, 400, map[string]string{"error": "插入位置无效"})
					return
				}
				a.queue = append(a.queue, QueueItem{})
				copy(a.queue[req.Index+1:], a.queue[req.Index:])
				a.queue[req.Index] = item
			} else {
				a.queue = append(a.queue, item)
			}
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
			index := -1
			for i := range a.queue {
				if a.queue[i].Key == req.Key {
					index = i
					break
				}
			}
			if index < 0 {
				send(w, 404, map[string]string{"error": "queue item not found"})
				return
			}
			item := &a.queue[index]
			manual := item.Platform == "manual" && (strings.HasPrefix(item.Key, "manual:") || strings.HasPrefix(item.Key, "admin:"))
			if (req.NameEdit != nil || req.SourcePlatform != nil) && !manual {
				send(w, 400, map[string]string{"error": "真实直播成员只允许修改备注，不允许更改认证姓名或来源"})
				return
			}
			// Validate all optional fields before mutating the queue entry.
			newName, newSource := item.Username, item.SourcePlatform
			if req.NameEdit != nil {
				newName = strings.TrimSpace(*req.NameEdit)
				if newName == "" || len([]rune(newName)) > 60 {
					send(w, 400, map[string]string{"error": "用户名必须为 1 到 60 个字符"})
					return
				}
			}
			if req.SourcePlatform != nil {
				newSource = strings.TrimSpace(*req.SourcePlatform)
				switch newSource {
				case "", "bilibili", "douyin", "huya", "wechat_mp", "kuaishou", "douyu":
				default:
					send(w, 400, map[string]string{"error": "未知的手动来源平台"})
					return
				}
			}
			item.Username, item.SourcePlatform = newName, newSource
			item.Note = strings.TrimSpace(req.Note)
		default:
			send(w, 400, map[string]string{"error": "unknown action"})
			return
		}
		if e := a.saveLocked(); e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.publishLocked(Event{Type: "queue", Data: a.queue})
		a.appendLogLocked("INFO", "queue", fmt.Sprintf("管理操作 %s，当前 %d 人，存档 %d", req.Action, len(a.queue), a.config.ArchiveSlot))
		send(w, 200, a.queue)
	})
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		send(w, 200, a.messages)
	})
	mux.HandleFunc("GET /api/events", a.events)
 	mux.HandleFunc("GET /api/update/versions", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {send(w,403,map[string]string{"error":"forbidden"});return}
		ctx,cancel:=context.WithTimeout(r.Context(),18*time.Second)
		defer cancel()
		rows,e:=a.updater.Versions(ctx)
		if e!=nil {send(w,502,map[string]string{"error":e.Error()});return}
		w.Header().Set("Cache-Control","no-store")
		send(w,200,rows)
	})
	mux.HandleFunc("GET /api/update/progress", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {send(w,403,map[string]string{"error":"forbidden"});return}
		a.progressMu.RLock()
		progress:=a.updateProgress
		a.progressMu.RUnlock()
		w.Header().Set("Cache-Control","no-store")
		send(w,200,progress)
	})
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		source := r.URL.Query().Get("source")
		if source == "" {
			source = "auto"
		}
		result, e := a.updater.Check(ctx, source)
		if e != nil {
			send(w, 502, map[string]string{"error": e.Error()})
			return
		}
		send(w, 200, result)
	})
	mux.HandleFunc("POST /api/update/download", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct {
			Source string `json:"source"`
			Version string `json:"version"`
			AllowDowngrade bool `json:"allow_downgrade"`
		}
		if e := decode(r, &body); e != nil {
			send(w, 400, map[string]string{"error": e.Error()})
			return
		}
		if !a.updateMu.TryLock(){send(w,409,map[string]string{"error":"已经有下载任务正在执行"});return}
		defer a.updateMu.Unlock()
		progressFn:=func(p update.DownloadProgress){
			a.progressMu.Lock()
			a.updateProgress=p
			a.progressMu.Unlock()
		}
		progressFn(update.DownloadProgress{Phase:"checking",Version:body.Version,Message:"正在检查 Release 和校验文件"})
		// Any newly initiated task invalidates the old install candidate.
		a.preparedUpdate=update.Downloaded{}
		ctx,cancel:=context.WithTimeout(r.Context(),4*time.Minute)
		defer cancel()
		result,e:=a.updater.DownloadVersion(ctx,body.Version,body.AllowDowngrade,progressFn,body.Source)
		if e!=nil{
			progressFn(update.DownloadProgress{Phase:"error",Version:body.Version,Message:e.Error()})
			send(w,502,map[string]string{"error":e.Error()});return
		}
		a.preparedUpdate=result
		send(w,200,result)
	})
	mux.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if a.installAction == nil {
			send(w, 409, map[string]string{"error": "当前运行方式不支持内置更新；Docker 请重建镜像"})
			return
		}
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		staged := a.preparedUpdate
		if staged.File == "" {
			send(w, 409, map[string]string{"error": "请先下载并校验更新包"})
			return
		}
		if e := a.installAction(staged); e != nil {
			send(w, 500, map[string]string{"error": e.Error()})
			return
		}
		a.preparedUpdate = update.Downloaded{}
		send(w, 200, map[string]string{"status": "restarting", "message": "更新助手已启动，服务即将重启"})
	})
	a.qrRoutes(mux)
	a.wsRoutes(mux)
	a.legacyRoutes(mux)
	a.storageRoutes(mux)
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
