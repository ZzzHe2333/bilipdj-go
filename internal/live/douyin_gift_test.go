package live

import (
    "bytes"
    "compress/gzip"
    "encoding/binary"
    "testing"
)

func pbNum(n int, v uint64) []byte {
    out := make([]byte, 0, 20)
    out = binary.AppendUvarint(out, uint64(n<<3))
    return binary.AppendUvarint(out, v)
}
func pbBlob(n int, data []byte) []byte {
    out := binary.AppendUvarint(nil, uint64(n<<3|2))
    out = binary.AppendUvarint(out, uint64(len(data)))
    return append(out, data...)
}
func testDouyinFrame(count int, end bool) []byte {
    common := append(pbNum(2, 12345), pbNum(3, 7698556487468780322)...)
    common = append(common, pbNum(4, 1792464867)...)
    user := append(pbNum(1, 787702714468572), pbBlob(3, []byte("test-user"))...)
    gift := append(pbNum(5, 3992), pbNum(12, 1)...)
    gift = append(gift, pbBlob(16, []byte("人气票"))...)
    payload := append(pbBlob(1, common), pbNum(2, 3992)...)
    payload = append(payload, pbNum(5, uint64(count))...)
    payload = append(payload, pbBlob(7, user)...)
    if end { payload = append(payload, pbNum(9, 1)...) }
    payload = append(payload, pbBlob(15, gift)...)
    entry := append(pbBlob(1, []byte("WebcastGiftMessage")), pbBlob(2, payload)...)
    response := pbBlob(1, entry)
    var out bytes.Buffer
    zw := gzip.NewWriter(&out)
    _, _ = zw.Write(response)
    _ = zw.Close()
    return pbBlob(8, out.Bytes())
}
func TestDouyinGiftPushFrame(t *testing.T) {
    updates, err := ParseDouyinGiftFrame(testDouyinFrame(520, true))
    if err != nil || len(updates) != 1 { t.Fatalf("decode failed: %+v: %v", updates, err) }
    g := updates[0]
    if g.GiftID != 3992 || g.Count != 520 || !g.RepeatEnd || g.RoomID != "7698556487468780322" || g.UnitDiamonds != 1 || g.GiftName != "人气票" || g.UserID != "787702714468572" {
        t.Fatalf("wrong gift: %+v", g)
    }
    if got, e := ParseDouyinGiftFrame([]byte(":hb")); e != nil || len(got) != 0 { t.Fatal("heartbeat must be ignored", e) }
    if _, err = ParseDouyinGiftFrame(make([]byte, maxDouyinPushFrame+1)); err == nil { t.Fatal("oversized frame accepted") }
}
