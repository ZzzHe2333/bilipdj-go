package live

import "testing"

func TestBiliGiftParser(t *testing.T) {
	sample := []byte(`{"cmd":"SEND_GIFT","data":{"uid":123,"uname":"Alice","giftName":"小花花","giftId":1,"num":2,"coin_type":"gold","tid":"tx123"}}`)
	event, ok := biliGiftMessage(sample)
	if !ok || event.Kind != "gift" || event.Gift == nil || event.Gift.Count != 2 || event.Gift.EventID != "tx123" || event.UserID != "123" {
		t.Fatalf("gift parser: %+v %v", event, ok)
	}
	guard, ok := biliGiftMessage([]byte(`{"cmd":"GUARD_BUY","data":{"uid":123,"uname":"Alice"}}`))
	if !ok || !guard.Gift.GuardBuy {
		t.Fatal("guard buy parse")
	}
	_, ok = biliGiftMessage([]byte(`{"cmd":"SEND_GIFT","data":{"uid":0,"uname":"Alice","giftName":"小花花"}}`))
	if ok {
		t.Fatal("zero UID must be rejected")
	}
	_, ok = biliGiftMessage([]byte(`{"cmd":"DANMU_MSG","data":{"uid":123,"uname":"Alice","giftName":"小花花"}}`))
	if ok {
		t.Fatal("spoofable command parsed")
	}
}
