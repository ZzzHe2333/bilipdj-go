package live

import (
	"bytes"
	"compress/zlib"
	"testing"
)

func TestBiliPacket(t *testing.T) {
	raw := []byte(`{"cmd":"DANMU_MSG","info":[[],"排队 一位",[123,"测试用户"]]}`)
	item := biliPacket(5, raw)
	var out []byte
	if e := biliWalk(item, 0, func(b []byte) { out = append([]byte(nil), b...) }); e != nil {
		t.Fatal(e)
	}
	if string(out) != string(raw) {
		t.Fatal("decode")
	}
	e, ok := biliMessage(out)
	if !ok || e.Username != "测试用户" || e.UserID != "123" {
		t.Fatalf("%+v %v", e, ok)
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(item)
	w.Close()
	wrapped := biliPacket(5, z.Bytes())
	wrapped[6] = 0
	wrapped[7] = 2
	var found int
	if err := biliWalk(wrapped, 0, func([]byte) { found++ }); err != nil || found != 1 {
		t.Fatalf("%v %d", err, found)
	}
}
