package core

import (
	"encoding/json"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"testing"
	"time"
)

func giftEvent(uid, name, tid, coin string) live.Event {
	return live.Event{Kind: "gift", Platform: "bilibili", UserID: uid, Username: "Alice", Time: time.Now(), Gift: &live.Gift{Name: name, EventID: tid, CoinType: coin, Count: 1}}
}
func TestGiftCreditsEligibilityAndPersistence(t *testing.T) {
	d := t.TempDir()
	a := New(d, "0.5", "test/repo")
	a.config.GiftQueue = &GiftSettings{Enabled: true, Names: []string{"小花花"}, SlotsPerGift: 2, InsertRank: 1}
	a.OnDanmu(giftEvent("123", "小花花", "tx-1", "gold"))
	if a.giftCredits["bilibili:123"] != 2 {
		t.Fatalf("not granted %+v", a.giftCredits)
	}
	a.OnDanmu(giftEvent("123", "小花花", "tx-1", "gold"))
	a.OnDanmu(giftEvent("123", "小花花", "tx-2", "gold"))
	if a.giftCredits["bilibili:123"] != 2 {
		t.Fatal("duplicated credit")
	}
	a = New(d, "0.5", "test/repo")
	if a.giftCredits["bilibili:123"] != 2 {
		t.Fatal("credits not persisted")
	}
	a.OnDanmu(live.Event{Platform: "bilibili", UserID: "123", Username: "Alice", Content: "插队", Time: time.Now()})
	if len(a.queue) != 1 || a.giftCredits["bilibili:123"] != 1 {
		t.Fatalf("spend %+v %+v", a.queue, a.giftCredits)
	}
	a.OnDanmu(live.Event{Platform: "bilibili", UserID: "123", Username: "Alice", Content: "取消排队", Time: time.Now()})
	a.OnDanmu(live.Event{Platform: "bilibili", UserID: "123", Username: "Alice", Content: "插队", Time: time.Now()})
	if len(a.queue) != 1 || a.giftCredits["bilibili:123"] != 0 {
		t.Fatal("second credit not spent")
	}
	a.OnDanmu(giftEvent("123", "小花花", "tx-3", "gold"))
	if a.giftCredits["bilibili:123"] != 0 {
		t.Fatal("one-time used user got credit again")
	}
	a = New(d, "0.5", "test/repo")
	if !a.giftUsed["bilibili:123"] || len(a.queue) != 1 {
		t.Fatal("used state not persisted")
	}
}
func TestGiftGoldBatteryAndOnlyMode(t *testing.T) {
	a := New(t.TempDir(), "0.5", "test")
	a.config.GiftQueue = &GiftSettings{Enabled: true, Names: []string{"小花花"}, MinBatteries: 100, SlotsPerGift: 1, InsertRank: 1, GiftOnly: true}
	a.OnDanmu(live.Event{Platform: "bilibili", Username: "Alice", UserID: "10", Content: "排队", Time: time.Now()})
	if len(a.queue) != 0 {
		t.Fatal("gift-only bypass")
	}
	a.OnDanmu(giftEvent("10", "小花花", "x1", "silver"))
	if len(a.giftCredits) != 0 {
		t.Fatal("silver gift earned credit")
	}
	a.OnDanmu(giftEvent("10", "不存在", "x2", "gold"))
	if len(a.giftCredits) != 0 {
		t.Fatal("unknown price earned credit")
	}
	a.OnDanmu(giftEvent("10", "小花花", "x3", "gold"))
	a.OnDanmu(live.Event{Platform: "bilibili", Username: "Alice", UserID: "10", Content: "插队", Time: time.Now()})
	if len(a.queue) != 1 {
		t.Fatal("gift-only insert missing")
	}
	a.OnDanmu(live.Event{Platform: "douyin", Username: "Alice", UserID: "10", Content: "插队", Time: time.Now()})
	if len(a.queue) != 1 {
		t.Fatal("douyin stole gift credit")
	}
}
func TestLegacyGiftMapping(t *testing.T) {
	y := []byte("myjs:\n  gift_queue_enabled: true\n  gift_queue_names:\n    - 小花花\n  gift_queue_min_batteries: 100\n  gift_queue_allow_multiple: true\n  gift_queue_slots_per_gift: 3\n  gift_queue_insert_rank: 2\n  gift_queue_only: true\n")
	m, e := parseLegacy("config.yaml", y, defaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	g := m.cfg.GiftQueue
	if !g.Enabled || g.Names[0] != "小花花" || g.MinBatteries != 100 || !g.AllowMultiple || g.SlotsPerGift != 3 || g.InsertRank != 2 || !g.GiftOnly {
		t.Fatalf("mapping %+v", g)
	}
	// Preview must not mutate the running config.
	a := defaultConfig()
	raw, _ := json.Marshal(a)
	_, _ = parseLegacy("config.yaml", y, a)
	after, _ := json.Marshal(a)
	if string(raw) != string(after) {
		t.Fatal("preview mutated original")
	}
}

func TestGiftCreditCannotBypassPause(t *testing.T) {
	a := New(t.TempDir(), "0.5", "test/repo")
	a.config.GiftQueue = &GiftSettings{Enabled: true, Names: []string{"小花花"}, SlotsPerGift: 1, InsertRank: 1}
	a.OnDanmu(giftEvent("99", "小花花", "gift99", "gold"))
	a.config.Switches.Paidui = false
	a.OnDanmu(live.Event{Platform: "bilibili", Username: "Alice", UserID: "99", Content: "插队", Time: time.Now()})
	if len(a.queue) != 0 || a.giftCredits["bilibili:99"] != 1 {
		t.Fatalf("paused bypass: %+v %+v", a.queue, a.giftCredits)
	}
}
