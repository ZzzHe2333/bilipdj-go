package core

import (
	"bytes"
	"encoding/json"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLegacyCommandsAndSwitches(t *testing.T) {
	a := New(t.TempDir(), "0.3.0", "test/repo")
	send := func(user, id, command string) {
		a.OnDanmu(live.Event{Platform: "bilibili", UserID: id, Username: user, Content: command, Time: time.Now()})
	}
	send("Alice", "1", "官服排 备注一")
	if len(a.queue) != 1 || a.queue[0].Mode != "官服" || a.queue[0].Note != "备注一" {
		t.Fatalf("special join %+v", a.queue)
	}
	send("Alice", "1", "修改 新备注")
	if a.queue[0].Note != "新备注" {
		t.Fatalf("modify %+v", a.queue)
	}
	send("Alice", "1", "排队")
	if len(a.queue) != 1 {
		t.Fatal("duplicate join")
	}
	send("Alice", "1", "取消排队")
	if len(a.queue) != 0 {
		t.Fatal("cancel")
	}
	a.config.Switches.Guanfu = false
	send("Alice", "1", "官服排")
	if len(a.queue) != 0 {
		t.Fatal("category switch ignored")
	}
	send("Alice", "1", "B服排")
	send("Bob", "2", "超级排")
	send("Carol", "3", "排米服")
	if len(a.queue) != 3 || a.queue[0].Mode != "B服" || a.queue[1].Mode != "超级" || a.queue[2].Mode != "米服" {
		t.Fatalf("category join %+v", a.queue)
	}
	a.config.Admins = []string{"Boss"}
	send("Guest", "4", "删除 1")
	if len(a.queue) != 3 {
		t.Fatal("unauthorized admin command")
	}
	send("Boss", "5", "删除 1")
	if len(a.queue) != 2 {
		t.Fatal("authorized delete")
	}
	send("Boss", "5", "完成")
	if len(a.queue) != 1 {
		t.Fatal("authorized finish")
	}
	a.config.Switches.Paidui = false
	send("New", "6", "排队")
	if len(a.queue) != 1 {
		t.Fatal("master switch ignored")
	}
}
func TestQueueSlotsPersistAndHTTP(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, "0.3.0", "test/repo")
	a.OnDanmu(live.Event{Platform: "douyin", UserID: "1", Username: "A", Content: "排队", Time: time.Now()})
	if len(a.queue) != 1 {
		t.Fatal("slot one")
	}
	handler := a.Routes(http.NotFoundHandler())
	send := func(slot int) int {
		buf, _ := json.Marshal(map[string]int{"slot": slot})
		req := httptest.NewRequest("POST", "http://127.0.0.1:9816/api/queue/slots", bytes.NewReader(buf))
		req.RemoteAddr = "127.0.0.1:1234"
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		return resp.Code
	}
	if code := send(11); code != 400 {
		t.Fatalf("accepted invalid slot %d", code)
	}
	if code := send(2); code != 200 {
		t.Fatalf("switch slot %d", code)
	}
	if len(a.queue) != 0 {
		t.Fatal("slot should initially be empty")
	}
	a.OnDanmu(live.Event{Platform: "douyin", UserID: "2", Username: "B", Content: "排队", Time: time.Now()})
	if code := send(1); code != 200 {
		t.Fatalf("back slot 1 %d", code)
	}
	if len(a.queue) != 1 || a.queue[0].Username != "A" {
		t.Fatal("slot data lost")
	}
	b := New(dir, "0.3.0", "test/repo")
	if len(b.queue) != 1 || b.queue[0].Username != "A" || len(b.slots["2"]) != 1 || b.slots["2"][0].Username != "B" {
		t.Fatalf("slot reload %+v %+v", b.queue, b.slots)
	}
}
