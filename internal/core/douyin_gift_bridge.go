package core

import (
    "crypto/rand"
    "crypto/subtle"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "net/http"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// The browser bridge is optional and read-only. A browser event can NEVER
// grant paid queue credits: an extension/page is not a payment authority.
const douyinBridgeMaxBody = 1500000
const douyinComboIdle = 4 * time.Second

type douyinBinding struct { liveID, roomID string }
type douyinCombo struct {
    update live.DouyinGiftUpdate
    count int
    lastSeen time.Time
    timer *time.Timer
}
type douyinClosed struct { createdAt int64; count int; finished time.Time }
type douyinGiftBridge struct {
    mu sync.Mutex
    token string
    bindings map[string]douyinBinding
    seen map[string]bool
    seenOrder []string
    pending map[string]*douyinCombo
    closed map[string]douyinClosed
}

func newDouyinGiftBridge() *douyinGiftBridge {
    b := &douyinGiftBridge{
        bindings: make(map[string]douyinBinding),
        seen: make(map[string]bool),
        pending: make(map[string]*douyinCombo),
        closed: make(map[string]douyinClosed),
    }
    raw := make([]byte, 32)
    if _, err := rand.Read(raw); err == nil {
        b.token = base64.RawURLEncoding.EncodeToString(raw)
    }
    return b
}

func douyinIsLoopback(r *http.Request) bool {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func (a *App) installDouyinBridgeRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /api/douyin/bridge/token", func(w http.ResponseWriter, r *http.Request) {
        if !douyinIsLoopback(r) || !a.isAdmin(r) {
            send(w, 403, map[string]string{"error": "loopback admin only"})
            return
        }
        w.Header().Set("Cache-Control", "no-store")
        send(w, 200, map[string]any{"token": a.douyinBridge.token, "endpoint": "/api/douyin/bridge/frame",
            "note": "Temporary pairing token; expires when BiliPDJ-GO restarts. Do not share."})
    })
    mux.HandleFunc("POST /api/douyin/bridge/frame", a.receiveDouyinGiftFrame)
}

func (a *App) bridgeListener(instance, liveID string) bool {
    a.mu.RLock()
    defer a.mu.RUnlock()
    for _, pc := range configuredListeners(a.config) {
        if pc.ID != instance || pc.Platform != "douyin" || !pc.Enabled { continue }
        normalized, err := live.ValidateDouyinRoom(pc.Room)
        return err == nil && normalized == liveID
    }
    return false
}

func (a *App) receiveDouyinGiftFrame(w http.ResponseWriter, r *http.Request) {
    if !douyinIsLoopback(r) || a.douyinBridge == nil || a.douyinBridge.token == "" {
        send(w, 403, map[string]string{"error": "local pairing required"})
        return
    }
    // Only extension service workers should supply a pairing token, not page JS.
    origin := r.Header.Get("Origin")
    if origin != "" && !strings.HasPrefix(origin, "chrome-extension://") {
        send(w, 403, map[string]string{"error": "invalid extension origin"})
        return
    }
    if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-PDJ-Bridge-Token")), []byte(a.douyinBridge.token)) != 1 {
        send(w, 401, map[string]string{"error": "invalid pairing token"})
        return
    }
    if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
        send(w, 415, map[string]string{"error": "JSON required"})
        return
    }
    body, err := io.ReadAll(io.LimitReader(r.Body, douyinBridgeMaxBody+1))
    if err != nil || len(body) > douyinBridgeMaxBody {
        send(w, 413, map[string]string{"error": "frame payload too large"})
        return
    }
    var req struct {
        InstanceID string `json:"instance_id"`
        LiveID string `json:"live_id"`
        Frame string `json:"frame"`
    }
    if json.Unmarshal(body, &req) != nil || req.Frame == "" || req.LiveID == "" || req.InstanceID == "" {
        send(w, 400, map[string]string{"error": "missing instance_id/live_id/frame"})
        return
    }
    if !a.bridgeListener(req.InstanceID, req.LiveID) {
        send(w, 409, map[string]string{"error": "configured enabled Douyin room does not match this tab"})
        return
    }
    bytes, err := base64.StdEncoding.DecodeString(req.Frame)
    if err != nil || len(bytes) == 0 || len(bytes) > (1<<20) {
        send(w, 400, map[string]string{"error": "invalid binary frame"})
        return
    }
    updates, err := live.ParseDouyinGiftFrame(bytes)
    if err != nil {
        send(w, 400, map[string]string{"error": "invalid PushFrame: " + err.Error()})
        return
    }
    if len(updates) == 0 { send(w, 200, map[string]int{"accepted": 0}); return }
    b := a.douyinBridge
    b.mu.Lock()
    binding := b.bindings[req.InstanceID]
    if binding.liveID != "" && binding.liveID != req.LiveID {
        // The app's room configuration has changed. Reset the binding only
        // after its listener configuration has been validated above.
        delete(b.bindings, req.InstanceID)
        binding = douyinBinding{}
    }
    for _, g := range updates {
        if binding.roomID != "" && binding.roomID != g.RoomID {
            b.mu.Unlock()
            send(w, 409, map[string]string{"error": "room ID changed during bridge session"})
            return
        }
        if binding.roomID == "" { binding = douyinBinding{req.LiveID, g.RoomID} }
    }
    b.bindings[req.InstanceID] = binding
    if len(b.closed) > 2048 {
        for key, completed := range b.closed {
            if time.Since(completed.finished) > 60*time.Second { delete(b.closed, key) }
        }
    }
    ready := make([]live.Event, 0)
    for _, g := range updates {
        if g.MessageID == "" { continue }
        msgKey := req.InstanceID + ":" + g.MessageID
        if b.seen[msgKey] { continue }
        b.seen[msgKey] = true
        b.seenOrder = append(b.seenOrder, msgKey)
        if len(b.seenOrder) > 8192 {
            delete(b.seen, b.seenOrder[0])
            b.seenOrder = b.seenOrder[1:]
        }
        key := douyinComboKey(req.InstanceID, g)
        if ended, exists := b.closed[key]; exists {
            if time.Since(ended.finished) > 60*time.Second {
                delete(b.closed, key)
            } else if !(g.RepeatEnd && g.Count == 1) &&
                (g.CreatedAt <= ended.createdAt ||
                    (g.Count <= ended.count && g.CreatedAt < ended.createdAt+2000)) {
                // Distinct single-gift messages can arrive in the same second;
                // each complete one-count event is not a duplicate combo.
                continue
            }
        }
        pending := b.pending[key]
        if pending == nil {
            pending = &douyinCombo{update:g}
            b.pending[key] = pending
        }
        if g.Count > pending.count {
            pending.count = g.Count
            pending.update = g
        }
        pending.lastSeen = time.Now()
        if pending.timer != nil { pending.timer.Stop() }
        if g.RepeatEnd {
            if pending.update.CreatedAt < g.CreatedAt { pending.update.CreatedAt = g.CreatedAt }
            if event, ok := finishDouyinCombo(req.InstanceID, pending); ok { ready = append(ready, event) }
            b.closed[key] = douyinClosed{createdAt: pending.update.CreatedAt, count: pending.count, finished: time.Now()}
            delete(b.pending, key)
        } else {
            pending.timer = time.AfterFunc(douyinComboIdle, func() { a.flushDouyinCombo(key, req.InstanceID, req.LiveID) })
        }
    }
    b.mu.Unlock()
    for _, event := range ready { a.emitDouyinBridgeGift(event, req.LiveID) }
    send(w, 200, map[string]int{"accepted": len(ready), "updates": len(updates)})
}

func douyinComboKey(instance string, g live.DouyinGiftUpdate) string {
    return instance + ":" + g.RoomID + ":" + g.UserID + ":" +
        strconv.FormatInt(g.GiftID, 10) + ":" + g.GroupID
}
func finishDouyinCombo(instance string, p *douyinCombo) (live.Event, bool) {
    g := p.update
    if p.count < 1 || g.UserID == "" || g.Username == "" || g.RoomID == "" { return live.Event{}, false }
    count := p.count
    // A meaningful event ID is per finalized combo, never per cumulative update.
    comboID := fmt.Sprintf("%s:%s:%s:%d:%d", g.RoomID, g.UserID, g.MessageID, g.GiftID, g.CreatedAt)
    return live.Event{
        Kind:"gift", Platform:"douyin", InstanceID:instance,
        UserID:g.UserID, Username:g.Username, Time:time.Now(),
        Gift:&live.Gift{EventID:comboID, Name:g.GiftName, GiftID:g.GiftID,
            Count:count, Diamonds:g.UnitDiamonds*int64(count), RoomID:g.RoomID},
    }, true
}
func (a *App) flushDouyinCombo(key, instance, liveID string) {
    b := a.douyinBridge
    b.mu.Lock()
    pending := b.pending[key]
    if pending == nil || time.Since(pending.lastSeen) < douyinComboIdle {
        b.mu.Unlock()
        return
    }
    event, ok := finishDouyinCombo(instance, pending)
    b.closed[key] = douyinClosed{createdAt: pending.update.CreatedAt, count: pending.count, finished: time.Now()}
    delete(b.pending, key)
    b.mu.Unlock()
    if ok { a.emitDouyinBridgeGift(event, liveID) }
}

func (a *App) emitDouyinBridgeGift(event live.Event, liveID string) {
    if !a.bridgeListener(event.InstanceID, liveID) { return }
    a.onDanmuGuarded(event, nil, 0)
}

