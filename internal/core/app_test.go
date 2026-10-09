package core

import (
	"bytes"
	"encoding/json"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueAndPersistence(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, "0.1.0", "org/demo")
	a.config.AutoQueue = true
	e := live.Event{Platform: "douyin", UserID: "1", Username: "测试", Content: "排队 带备注", Time: time.Now()}
	a.OnDanmu(e)
	a.OnDanmu(e)
	if len(a.queue) != 1 || a.queue[0].Note != "带备注" {
		t.Fatalf("unexpected queue %+v", a.queue)
	}
	b := New(dir, "0.1.0", "org/demo")
	if len(b.queue) != 1 || b.queue[0].Username != "测试" {
		t.Fatalf("persistence %+v", b.queue)
	}
	if file, e := os.Stat(filepath.Join(dir, "state.json")); e != nil || file.Mode().Perm() != 0600 {
		t.Fatalf("file security: %v %+v", e, file)
	}
}
func TestHTTPAndSecurity(t *testing.T) {
	a := New(t.TempDir(), "0.1.0", "org/demo")
	handler := a.Routes(http.NotFoundHandler())
	post := func(origin, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9816/api/queue", bytes.NewBufferString(`{"action":"add","name":"Alice"}`))
		req.RemoteAddr = remote
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := post("http://attacker.tld", "127.0.0.1:9999"); w.Code != 403 {
		t.Fatalf("origin security: %d", w.Code)
	}
	if w := post("http://127.0.0.1:9816", "192.168.1.2:9999"); w.Code != 403 {
		t.Fatalf("remote security: %d", w.Code)
	}
	if w := post("http://127.0.0.1:9816", "127.0.0.1:9999"); w.Code != 200 {
		t.Fatalf("valid admin: %d %s", w.Code, w.Body)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/queue", nil)
	handler.ServeHTTP(w, req)
	var items []QueueItem
	if e := json.Unmarshal(w.Body.Bytes(), &items); e != nil || len(items) != 1 {
		t.Fatalf("queue HTTP: %v %+v", e, items)
	}
}


func TestQueueEventsAreImmutableSnapshots(t *testing.T) {
	a := New(t.TempDir(), "0.10.1", "org/demo")
	events := make(chan Event, 1)
	a.mu.Lock()
	a.subscribers[events] = struct{}{}
	a.queue = []QueueItem{{Key: "douyin:123", Platform: "douyin", Username: "before"}}
	a.publishLocked(Event{Type: "queue", Data: a.queue})
	// In-place edits after publishing must never change a queued SSE event.
	a.queue[0].Username = "after"
	a.mu.Unlock()
	select {
	case event := <-events:
		entries, ok := event.Data.([]QueueItem)
		if !ok || len(entries) != 1 || entries[0].Username != "before" {
			t.Fatalf("mutable queue event leaked: %+v", event.Data)
		}
	default:
		t.Fatal("expected queue event")
	}
}
