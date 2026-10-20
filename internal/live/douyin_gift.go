package live

import (
    "bytes"
    "compress/gzip"
    "errors"
    "fmt"
    "io"
    "strings"
)

// DouyinGiftUpdate is one cumulative update from a live WebcastGiftMessage.
// Count is a cumulative combo value, NOT a standalone payment.
type DouyinGiftUpdate struct {
    MessageID string
    RoomID string
    CreatedAt int64 // milliseconds since epoch when known
    UserID string
    Username string
    GiftID int64
    GiftName string
    UnitDiamonds int64
    Count int
    GroupID string
    RepeatEnd bool
}

const maxDouyinPushFrame = 1 << 20
const maxDouyinInflatedResponse = 4 << 20

// ParseDouyinGiftFrame decodes an observed *server* PushFrame from the logged-in
// Douyin web client's WSS stream. It cannot establish an authenticated session.
func ParseDouyinGiftFrame(frame []byte) ([]DouyinGiftUpdate, error) {
    if len(frame) == 0 || len(frame) > maxDouyinPushFrame {
        return nil, errors.New("invalid Douyin PushFrame size")
    }
    // Heartbeats are not protobuf business frames.
    if bytes.Equal(frame, []byte(":hb")) { return nil, nil }
    var payload []byte
    if err := pbWalk(frame, func(f pbField) error {
        if f.N == 8 && f.Wire == 2 { payload = f.Bytes }
        return nil
    }); err != nil { return nil, fmt.Errorf("pushframe: %w", err) }
    if len(payload) == 0 { return nil, nil }
    if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
        zr, err := gzip.NewReader(bytes.NewReader(payload))
        if err != nil { return nil, err }
        inflated, err := io.ReadAll(io.LimitReader(zr, maxDouyinInflatedResponse+1))
        closeErr := zr.Close()
        if err != nil { return nil, err }
        if closeErr != nil { return nil, closeErr }
        if len(inflated) > maxDouyinInflatedResponse { return nil, errors.New("Douyin gzip response too large") }
        payload = inflated
    }
    updates := make([]DouyinGiftUpdate, 0)
    err := pbWalk(payload, func(f pbField) error {
        if f.N != 1 || f.Wire != 2 { return nil }
        var method string
        var body []byte
        if err := pbWalk(f.Bytes, func(m pbField) error {
            if m.Wire == 2 {
                if m.N == 1 { method = string(m.Bytes) }
                if m.N == 2 { body = m.Bytes }
            }
            return nil
        }); err != nil { return err }
        if method != "WebcastGiftMessage" { return nil }
        update, err := parseDouyinGiftPayload(body)
        if err != nil { return err }
        if update.UserID != "" && update.GiftID > 0 && update.RoomID != "" &&
            update.GiftName != "" && len(update.Username) <= 512 &&
            update.Count >= 1 && update.Count <= 10000000 &&
            update.UnitDiamonds >= 0 && update.UnitDiamonds <= 1000000000 {
            updates = append(updates, update)
        }
        if len(updates) > 1000 { return errors.New("too many gift updates per frame") }
        return nil
    })
    return updates, err
}

func parseDouyinGiftPayload(raw []byte) (DouyinGiftUpdate, error) {
    g := DouyinGiftUpdate{Count: 1}
    var common, user, gift []byte
    err := pbWalk(raw, func(f pbField) error {
        switch f.N {
        case 1: if f.Wire == 2 { common = f.Bytes }
        case 2: if f.Wire == 0 { g.GiftID = int64(f.Value) }
        case 5: if f.Wire == 0 && f.Value > 0 && f.Value < 100000000 { g.Count = int(f.Value) }
        case 7: if f.Wire == 2 { user = f.Bytes }
        case 9: if f.Wire == 0 { g.RepeatEnd = f.Value == 1 }
        case 10: if f.Wire == 0 { g.GroupID = fmt.Sprint(f.Value) }
        case 15: if f.Wire == 2 { gift = f.Bytes }
        }
        return nil
    })
    if err != nil { return g, err }
    err = pbWalk(common, func(f pbField) error {
        switch f.N {
        case 2: if f.Wire == 0 { g.MessageID = fmt.Sprint(f.Value) }
        case 3: if f.Wire == 0 { g.RoomID = fmt.Sprint(f.Value) }
        case 4: if f.Wire == 0 {
            g.CreatedAt = int64(f.Value)
            if g.CreatedAt < 100000000000 { g.CreatedAt *= 1000 }
        }
        }
        return nil
    })
    if err != nil { return g, err }
    err = pbWalk(user, func(f pbField) error {
        if f.N == 1 && f.Wire == 0 { g.UserID = fmt.Sprint(f.Value) }
        if f.N == 3 && f.Wire == 2 { g.Username = strings.TrimSpace(string(f.Bytes)) }
        return nil
    })
    if err != nil { return g, err }
    err = pbWalk(gift, func(f pbField) error {
        switch f.N {
        case 5: if f.Wire == 0 && g.GiftID == 0 { g.GiftID = int64(f.Value) }
        case 12: if f.Wire == 0 { g.UnitDiamonds = int64(f.Value) }
        case 16: if f.Wire == 2 { g.GiftName = strings.TrimSpace(string(f.Bytes)) }
        }
        return nil
    })
    return g, err
}
