package live

import (
	"errors"
	"fmt"
	"time"
)

// pbField implements minimal protobuf wire-format decoding (varint and length-delimited).
// It bounds all slices and rejects malformed data; unrecognized fields are skipped.
type pbField struct {
	N     int
	Wire  int
	Value uint64
	Bytes []byte
}

func pbWalk(raw []byte, visit func(pbField) error) error {
	for len(raw) > 0 {
		tag, n, err := pbVarint(raw)
		if err != nil {
			return err
		}
		raw = raw[n:]
		field := pbField{N: int(tag >> 3), Wire: int(tag & 7)}
		if field.N == 0 {
			return errors.New("protobuf field 0")
		}
		switch field.Wire {
		case 0:
			field.Value, n, err = pbVarint(raw)
			if err != nil {
				return err
			}
			raw = raw[n:]
		case 1:
			if len(raw) < 8 {
				return errors.New("truncated fixed64")
			}
			raw = raw[8:]
		case 2:
			var v uint64
			v, n, err = pbVarint(raw)
			if err != nil {
				return err
			}
			raw = raw[n:]
			if v > uint64(len(raw)) {
				return errors.New("protobuf field exceeds buffer")
			}
			field.Bytes = raw[:int(v)]
			raw = raw[int(v):]
		case 5:
			if len(raw) < 4 {
				return errors.New("truncated fixed32")
			}
			raw = raw[4:]
		default:
			return fmt.Errorf("unsupported protobuf wire %d", field.Wire)
		}
		if err = visit(field); err != nil {
			return err
		}
	}
	return nil
}
func pbVarint(in []byte) (uint64, int, error) {
	var x uint64
	for i := 0; i < 10 && i < len(in); i++ {
		b := in[i]
		if i == 9 && b > 1 {
			return 0, 0, errors.New("protobuf varint overflow")
		}
		x |= uint64(b&0x7f) << (7 * i)
		if b < 128 {
			return x, i + 1, nil
		}
	}
	return 0, 0, errors.New("truncated protobuf varint")
}

type pollResult struct {
	Cursor, Ext string
	Interval    time.Duration
	Events      []Event
}

func parseDouyinResponse(raw []byte) (pollResult, error) {
	result := pollResult{Interval: time.Second, Events: make([]Event, 0)}
	err := pbWalk(raw, func(f pbField) error {
		switch f.N {
		case 1:
			if f.Wire == 2 {
				if e, ok := parseDouyinEntry(f.Bytes); ok {
					result.Events = append(result.Events, e)
				}
			}
		case 2:
			if f.Wire == 2 {
				result.Cursor = string(f.Bytes)
			}
		case 3:
			if f.Wire == 0 && f.Value > 0 {
				result.Interval = time.Duration(f.Value) * time.Millisecond
			}
		case 5:
			if f.Wire == 2 {
				result.Ext = string(f.Bytes)
			}
		}
		return nil
	})
	return result, err
}
func parseDouyinEntry(raw []byte) (Event, bool) {
	var method string
	var payload []byte
	if pbWalk(raw, func(f pbField) error {
		if f.Wire == 2 {
			switch f.N {
			case 1:
				method = string(f.Bytes)
			case 2:
				payload = f.Bytes
			}
		}
		return nil
	}) != nil || method != "WebcastChatMessage" {
		return Event{}, false
	}
	var user []byte
	var content string
	if pbWalk(payload, func(f pbField) error {
		if f.Wire == 2 {
			switch f.N {
			case 2:
				user = f.Bytes
			case 3:
				content = string(f.Bytes)
			}
		}
		return nil
	}) != nil {
		return Event{}, false
	}
	var name, id, secUID string
	if pbWalk(user, func(f pbField) error {
		switch f.N {
		case 1:
			if f.Wire == 0 {
				id = fmt.Sprint(f.Value)
			}
		case 3:
			if f.Wire == 2 {
				name = string(f.Bytes)
			}
		case 46:
			if f.Wire == 2 {
				secUID = string(f.Bytes)
			}
		}
		return nil
	}) != nil {
		return Event{}, false
	}
	if name == "" || content == "" {
		return Event{}, false
	}
	if secUID != "" {
		id = secUID
	}
	return Event{Platform: "douyin", UserID: id, Username: name, Content: content, Time: time.Now()}, true
}
