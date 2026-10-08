package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func TestDailyQuotaPersistsResetAndPlatformIsolation(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, "0.4.0", "test/repo")
	a.config.DailyQueueLimit = 1
	a.config.DailyQueueResetTime = "04:00"
	base := time.Date(2026, 10, 8, 3, 59, 0, 0, time.Local)
	send := func(p, uid, user, cmd string, ts time.Time) {
		a.OnDanmu(live.Event{Platform: p, UserID: uid, Username: user, Content: cmd, Time: ts})
	}
	send("bilibili", "10", "Amy", "排队", base)
	if len(a.queue) != 1 || a.dailyCounts["bilibili:10"] != 1 {
		t.Fatalf("first join %+v", a.dailyCounts)
	}
	send("bilibili", "10", "Amy", "取消排队", base)
	send("bilibili", "10", "Amy", "排队", base)
	if len(a.queue) != 0 {
		t.Fatal("quota bypass after cancel")
	}
	b := New(dir, "0.4.0", "test/repo")
	if b.dailyCounts["bilibili:10"] != 1 {
		t.Fatal("quota not persisted")
	}
	b.OnDanmu(live.Event{Platform: "bilibili", UserID: "10", Username: "Amy", Content: "排队", Time: base})
	if len(b.queue) != 0 {
		t.Fatal("quota bypass after restart")
	}
	b.OnDanmu(live.Event{Platform: "douyin", UserID: "10", Username: "Amy", Content: "排队", Time: base})
	if len(b.queue) != 1 {
		t.Fatal("platform identity collided")
	}
	b.OnDanmu(live.Event{Platform: "bilibili", UserID: "10", Username: "Amy", Content: "排队", Time: base.Add(2 * time.Minute)})
	if len(b.queue) != 2 || b.dailyCounts["bilibili:10"] != 1 || b.dailyPeriod != "2026-10-08@04:00" {
		t.Fatalf("reset failed %+v period=%s", b.dailyCounts, b.dailyPeriod)
	}
	if b.dailyPeriodAt(base) != "2026-10-07@04:00" {
		t.Fatal("reset boundary incorrect")
	}
}

func TestRolePermissionsBlacklistAndSwitchPersistence(t *testing.T) {
	a := New(t.TempDir(), "0.4.0", "test/repo")
	a.config.SuperAdmins = []string{"chief"}
	a.config.Admins = []string{"admin"}
	send := func(u string, actor live.Event) {
		actor.Username = u
		actor.Platform = "bilibili"
		if actor.UserID == "" {
			actor.UserID = u
		}
		actor.Time = time.Now()
		a.OnDanmu(actor)
	}
	send("viewer", live.Event{Content: "添加管理员 Eve"})
	if named(a.config.Admins, "Eve") {
		t.Fatal("ordinary viewer escalated")
	}
	send("admin", live.Event{Content: "添加管理员 Eve"})
	if named(a.config.Admins, "Eve") {
		t.Fatal("regular admin escalated")
	}
	send("chief", live.Event{Content: "添加管理员 Eve"})
	if !named(a.config.Admins, "Eve") {
		t.Fatal("super admin could not add admin")
	}
	send("Eve", live.Event{Content: "暂停排队功能"})
	if a.config.Switches.Paidui {
		t.Fatal("admin did not pause")
	}
	send("viewer", live.Event{Content: "排队"})
	if len(a.queue) != 0 {
		t.Fatal("master switch did not block join")
	}
	send("admin", live.Event{Content: "恢复排队功能"})
	send("mod", live.Event{Content: "新增 不应该成功", IsRoomAdmin: true})
	if len(a.queue) != 0 {
		t.Fatal("room admin bypassed disabled switch")
	}
	send("chief", live.Event{Content: "允许房管成为插件管理员"})
	send("mod", live.Event{Content: "新增 房管队列", IsRoomAdmin: true})
	if len(a.queue) != 1 {
		t.Fatal("enabled room admin could not operate")
	}
	send("chief", live.Event{Content: "拉黑 Eve"})
	if !named(a.config.Blacklist, "Eve") || named(a.config.Admins, "Eve") {
		t.Fatal("blacklisted admin still active")
	}
	send("Eve", live.Event{Content: "删除 1"})
	if len(a.queue) != 1 {
		t.Fatal("blacklisted operator acted")
	}
	send("realAnchor", live.Event{Content: "删除 1", IsAnchor: true})
	if len(a.queue) != 0 {
		t.Fatal("verified anchor cannot operate")
	}
	// An 'anchor' flag from Douyin is not trusted by this Bilibili-only check.
	a.OnDanmu(live.Event{Platform: "douyin", UserID: "fake", Username: "random", IsAnchor: true, Content: "新增 fake", Time: time.Now()})
	if len(a.queue) != 0 {
		t.Fatal("unverified cross-platform anchor escalated")
	}
	got := New(filepath.Dir(a.dataPath), "0.4.0", "test/repo")
	if !got.config.Switches.RoomAdminOperator || !named(got.config.Blacklist, "Eve") {
		t.Fatal("permissions not saved")
	}
}

func TestGuardInsertRequiresVerifiedLevelOrConfiguredName(t *testing.T) {
	a := New(t.TempDir(), "0.4.0", "test/repo")
	a.config.Switches.GuardInsert = true
	on := func(platform, id, user, cmd string, level int) {
		a.OnDanmu(live.Event{Platform: platform, UserID: id, Username: user, Content: cmd, GuardLevel: level, Time: time.Now()})
	}
	on("bilibili", "1", "A", "排队", 0)
	on("bilibili", "2", "G1", "插队", 3)
	on("bilibili", "3", "G2", "插队", 1)
	on("bilibili", "4", "B", "排队", 0)
	on("bilibili", "5", "G3", "插队", 2)
	if len(a.queue) != 5 {
		t.Fatalf("missing guard %+v", a.queue)
	}
	// Legacy appends a new guard immediately before the consecutive trailing guards.
	// B joined after G2 so the trailing guard run was empty: G3 goes at end.
	wants := []string{"A", "G2", "G1", "B", "G3"}
	for i, want := range wants {
		if a.queue[i].Username != want {
			t.Fatalf("guard ordering got %+v", a.queue)
		}
	}
	a.config.Switches.GuardInsert = false
	on("bilibili", "6", "G4", "插队", 3)
	if len(a.queue) != 5 {
		t.Fatal("disabled guard insertion bypass")
	}
	a.config.Switches.GuardInsert = true
	on("douyin", "7", "spoof", "插队", 3)
	if len(a.queue) != 5 {
		t.Fatal("douyin role should not be trusted as Bilibili guard")
	}
}

func TestLegacyDailyAndPermissionSettingsImport(t *testing.T) {
	a := New(t.TempDir(), "0.4.0", "test/repo")
	body := []byte(`myjs:
  daily_queue_limit: 2
  daily_queue_reset_time: "05:30"
  daily_queue_period: "2026-10-07@05:30"
  daily_queue_counts:
    - "987=1"
quanxian:
  super_admin:
    - "Root"
  admin:
    - "Helper"
  jianzhang:
    - "Guard"
kaiguan:
  jianzhang_chadui: true
  fangguan_op: true
`)
	result, e := parseLegacy("config.yaml", body, a.config)
	if e != nil {
		t.Fatal(e)
	}
	if result.cfg.DailyQueueLimit != 2 || result.cfg.DailyQueueResetTime != "05:30" || result.dailyCounts["bilibili:987"] != 1 || !named(result.cfg.SuperAdmins, "Root") || !named(result.cfg.Guards, "Guard") || !result.cfg.Switches.GuardInsert || !result.cfg.Switches.RoomAdminOperator {
		t.Fatalf("incomplete import: %+v, daily=%v", result.cfg, result.dailyCounts)
	}
	if a.config.DailyQueueLimit != 0 || a.config.Switches.GuardInsert {
		t.Fatal("preview mutated live state")
	}
	handler := a.Routes(http.NotFoundHandler())
	var multipart bytes.Buffer
	// Import in isolation from the real source, using the same route as the Vue UI.
	boundary := "simpleBoundary"
	multipart.WriteString("--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"config.yaml\"\r\nContent-Type: application/octet-stream\r\n\r\n")
	multipart.Write(body)
	multipart.WriteString("\r\n--" + boundary + "--\r\n")
	req := httptest.NewRequest("POST", "http://127.0.0.1:9816/api/legacy/import", &multipart)
	req.RemoteAddr = "127.0.0.1:8888"
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("import API: %d %s", rec.Code, rec.Body.String())
	}
	if a.config.DailyQueueLimit != 2 || a.dailyCounts["bilibili:987"] != 1 {
		t.Fatal("import did not persist daily rules")
	}
	cfgReq := httptest.NewRequest("GET", "http://127.0.0.1:9816/api/config", nil)
	cfgRec := httptest.NewRecorder()
	handler.ServeHTTP(cfgRec, cfgReq)
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(cfgRec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cfgRec.Body.Bytes(), []byte("daily_counts")) {
		t.Fatal("daily user IDs leaked via public config")
	}
}
