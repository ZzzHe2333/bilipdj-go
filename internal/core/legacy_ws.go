package core

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZzzHe2333/bilipdj-go/internal/live"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const maxLegacyWSFrame = 1 << 20

type legacyWS struct {
	conn  net.Conn
	input *bufio.Reader
	mu    sync.Mutex
}

func (c *legacyWS) send(op byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(payload) > maxLegacyWSFrame {
		return errors.New("WS 输出过大")
	}
	h := []byte{0x80 | op}
	if len(payload) < 126 {
		h = append(h, byte(len(payload)))
	} else if len(payload) <= 0xffff {
		h = append(h, 126)
		h = binary.BigEndian.AppendUint16(h, uint16(len(payload)))
	} else {
		h = append(h, 127)
		h = binary.BigEndian.AppendUint64(h, uint64(len(payload)))
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	for _, section := range [][]byte{h, payload} {
		for len(section) > 0 {
			n, e := c.conn.Write(section)
			if e != nil {
				return e
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			section = section[n:]
		}
	}
	return nil
}
func (c *legacyWS) sendJSON(obj any) error {
	b, e := json.Marshal(obj)
	if e != nil {
		return e
	}
	return c.send(1, b)
}
func (c *legacyWS) readLoop(done chan<- struct{}) {
	defer close(done)
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		h := make([]byte, 2)
		if _, e := io.ReadFull(c.input, h); e != nil {
			return
		}
		if h[0]&0x70 != 0 || h[0]&0x80 == 0 || h[1]&0x80 == 0 {
			return
		} // Reject unmasked and fragmented client frames.
		op := h[0] & 0xf
		size := uint64(h[1] & 0x7f)
		if size == 126 {
			var n [2]byte
			if _, e := io.ReadFull(c.input, n[:]); e != nil {
				return
			}
			size = uint64(binary.BigEndian.Uint16(n[:]))
		}
		if size == 127 {
			var n [8]byte
			if _, e := io.ReadFull(c.input, n[:]); e != nil {
				return
			}
			size = binary.BigEndian.Uint64(n[:])
		}
		if size > 64<<10 || (op >= 8 && size > 125) {
			return
		}
		var mask [4]byte
		if _, e := io.ReadFull(c.input, mask[:]); e != nil {
			return
		}
		payload := make([]byte, int(size))
		if _, e := io.ReadFull(c.input, payload); e != nil {
			return
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch op {
		case 8:
			_ = c.send(8, []byte{})
			return
		case 9:
			if c.send(10, payload) != nil {
				return
			}
		case 10: // pong
		case 1, 2: // Read-only bridge: never execute or relay client-sent commands.
		default:
			return
		}
	}
}
func checkWSOrigin(r *http.Request) bool {
	raw := r.Header.Get("Origin")
	if raw == "" {
		return true
	}
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func legacyEntry(q QueueItem) map[string]string {
	return map[string]string{"id": queueDisplayName(q), "content": q.Note, "platform": q.Platform, "user_id": q.UserID}
}
func legacyQueuePayload(q []QueueItem) any {
	queue := make([]string, 0, len(q))
	entries := make([]map[string]string, 0, len(q))
	for _, item := range q {
		name := queueDisplayName(item)
		if item.Mode != "" {
			name = item.Mode + "|" + name
		}
		if item.Note != "" {
			name += " " + item.Note
		}
		queue = append(queue, name)
		entries = append(entries, legacyEntry(item))
	}
	return map[string]any{"type": "QUEUE_UPDATE", "queue": queue, "entries": entries}
}
func legacyMessage(ev Event) any {
	switch ev.Type {
	case "queue":
		if q, ok := ev.Data.([]QueueItem); ok {
			return legacyQueuePayload(q)
		}
	case "status":
		if s, ok := ev.Data.(live.Status); ok {
			state := "danmu_disconnected"
			if s.Connected {
				state = "danmu_connected"
			}
			return map[string]any{"type": "PDJ_STATUS", "platform": s.Platform, "status": state, "message": s.Message, "connected": s.Connected}
		}
	case "danmu":
		if e, ok := ev.Data.(live.Event); ok {
			if e.Platform == "douyin" {
				return map[string]any{"type": "DOUYIN_DANMU", "uid": e.UserID, "nickname": e.Username, "content": e.Content, "platform": "douyin", "time": e.Time}
			}
			if e.Platform == "bilibili" {
				userID := any(e.UserID)
				if uid, err := strconv.ParseInt(e.UserID, 10, 64); err == nil && uid > 0 {
					userID = uid
				}
				return map[string]any{"cmd": "DANMU_MSG", "info": []any{[]any{}, e.Content, []any{userID, e.Username}}, "platform": "bilibili", "time": e.Time}
			}
		}
	}
	return nil
}
func (a *App) wsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		if !checkWSOrigin(r) {
			http.Error(w, "invalid WebSocket origin", http.StatusForbidden)
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		k, e := base64.StdEncoding.DecodeString(key)
		if e != nil || len(k) != 16 || r.Header.Get("Sec-WebSocket-Version") != "13" || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
			http.Error(w, "invalid WebSocket handshake", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "WebSocket unavailable", 500)
			return
		}
		conn, buf, e := hj.Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		accept := sha1.Sum([]byte(key + wsGUID))
		if _, e = fmt.Fprintf(buf, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:])); e != nil {
			return
		}
		if e = buf.Flush(); e != nil {
			return
		}
		ws := &legacyWS{conn: conn, input: buf.Reader}
		ch := make(chan Event, 64)
		a.mu.Lock()
		a.subscribers[ch] = struct{}{}
		snapshot := append([]QueueItem{}, a.queue...)
		statuses := make([]live.Status, 0, len(a.statuses))
		for _, s := range a.statuses {
			statuses = append(statuses, s)
		}
		a.mu.Unlock()
		defer func() { a.mu.Lock(); delete(a.subscribers, ch); a.mu.Unlock() }()
		if ws.sendJSON(map[string]any{"type": "PDJ_STATUS", "status": "connected", "message": "Go WebSocket compatibility bridge ready"}) != nil {
			return
		}
		if ws.sendJSON(legacyQueuePayload(snapshot)) != nil {
			return
		}
		for _, s := range statuses {
			if ws.sendJSON(legacyMessage(Event{Type: "status", Data: s})) != nil {
				return
			}
		}
		closed := make(chan struct{})
		go ws.readLoop(closed)
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-closed:
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if ws.send(9, []byte("ping")) != nil {
					return
				}
			case evt := <-ch:
				if msg := legacyMessage(evt); msg != nil {
					if ws.sendJSON(msg) != nil {
						return
					}
				}
			}
		}
	})
}
