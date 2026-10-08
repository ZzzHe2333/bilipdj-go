package live

import (
	"encoding/binary"
	"testing"
)

func vfield(n int, v uint64) []byte { return append(pv(uint64(n<<3)), pv(v)...) }
func pv(v uint64) []byte {
	b := make([]byte, binary.MaxVarintLen64)
	k := binary.PutUvarint(b, v)
	return b[:k]
}
func sfield(n int, b []byte) []byte {
	x := pv(uint64(n<<3 | 2))
	x = append(x, pv(uint64(len(b)))...)
	return append(x, b...)
}
func TestDouyinDecode(t *testing.T) {
	user := append(vfield(1, 100), sfield(3, []byte("测试用户"))...)
	chat := append(sfield(2, user), sfield(3, []byte("排队 我来了"))...)
	msg := append(sfield(1, []byte("WebcastChatMessage")), sfield(2, chat)...)
	raw := append(append(sfield(1, msg), sfield(2, []byte("CURSOR"))...), vfield(3, 800)...)
	r, e := parseDouyinResponse(raw)
	if e != nil || len(r.Events) != 1 {
		t.Fatalf("%v %+v", e, r)
	}
	if r.Events[0].UserID != "100" || r.Events[0].Content != "排队 我来了" || r.Cursor != "CURSOR" {
		t.Fatalf("wrong message: %+v", r)
	}
}
func TestRejectMalformed(t *testing.T) {
	if _, e := parseDouyinResponse([]byte{0x0a, 0xff}); e == nil {
		t.Fatal("expected error")
	}
}
