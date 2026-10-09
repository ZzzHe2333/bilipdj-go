package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func TestConsoleLogSanitizationBoundAndSSE(t *testing.T) {
	a := New(t.TempDir(), "0.7.0", "org/example")
	ch := make(chan Event, 16)
	a.mu.Lock()
	a.subscribers[ch] = struct{}{}
	a.appendLogLocked("INFO", "system", "Cookie: xxx; SESSDATA=abc; ticket=xyz")
	a.mu.Unlock()
	ev := <-ch
	if ev.Type != "log" {
		t.Fatalf("unexpected event %s", ev.Type)
	}
	log := ev.Data.(LogEntry)
	if strings.Contains(log.Message, "xxx") || strings.Contains(log.Message, "abc") || strings.Contains(log.Message, "xyz") {
		t.Fatalf("log disclosed credentials: %q", log.Message)
	}
	for i := 0; i < 600; i++ {
		a.logEvent("INFO", "queue", "队列已更新")
	}
	if len(a.logs) != maxConsoleLogs || a.logs[0].ID == 0 || a.logs[0].ID >= a.logs[len(a.logs)-1].ID {
		t.Fatalf("bounded log broken, count=%d", len(a.logs))
	}
	a.publishStatus(live.Status{Platform: "bilibili", Connected: true, Message: "监听已连接"})
	if a.logs[len(a.logs)-1].Category != "bilibili" {
		t.Fatal("status did not reach console log")
	}
}

func TestConsoleLogHTTPAndQueueInsert(t *testing.T) {
	a := New(t.TempDir(), "0.7.0", "org/example")
	h := a.Routes(http.NotFoundHandler())
	call := func(method, path, body, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:9816"+path, bytes.NewBufferString(body))
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := call("GET", "/api/logs", "", "203.0.113.4:1000"); w.Code != 403 {
		t.Fatalf("logs unauthorized: %d", w.Code)
	}
	if w := call("GET", "/api/logs", "", "127.0.0.1:9000"); w.Code != 200 {
		t.Fatalf("logs local: %d", w.Code)
	}
	post := func(body string) {
		t.Helper()
		if w := call("POST", "/api/queue", body, "127.0.0.1:9000"); w.Code != 200 {
			t.Fatalf("queue mutation status=%d, body=%s", w.Code, w.Body.String())
		}
	}
	post(`{"action":"add","name":"首位"}`)
	post(`{"action":"add","name":"末位"}`)
	post(`{"action":"insert","name":"中间","index":1}`)
	if got := []string{a.queue[0].Username, a.queue[1].Username, a.queue[2].Username}; got[0] != "首位" || got[1] != "中间" || got[2] != "末位" {
		t.Fatalf("wrong insert order: %v", got)
	}
	if w := call("POST", "/api/queue", `{"action":"insert","name":"错误位置","index":99}`, "127.0.0.1:9000"); w.Code != 400 {
		t.Fatalf("accepted invalid index: %d", w.Code)
	}
	if len(a.queue) != 3 {
		t.Fatalf("queue mutated on failure: %d", len(a.queue))
	}
	w := call("GET", "/api/logs", "", "127.0.0.1:9000")
	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil || len(entries) < 4 {
		t.Fatalf("logs API: %v %+v", err, entries)
	}
	for _, entry := range entries {
		if entry.Message == "" {
			t.Fatal("blank log entry")
		}
	}
}
