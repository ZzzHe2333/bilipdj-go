package live

// Minimal RFC 6455 client for the Bilibili /sub binary stream. Keeping the
// client in the standard library allows fully static cross-platform builds.
import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const biliMaxMessage = 8 << 20

// biliStream abstracts framed WSS and unframed TCP so they share identical
// authentication, heartbeat and packet processing.
type biliStream interface {
	ReadPacket() ([]byte, error)
	WritePacket([]byte) error
	Close() error
}

type biliTCP struct{ net.Conn }

func (b *biliTCP) ReadPacket() ([]byte, error) {
	h := make([]byte, 16)
	if _, err := io.ReadFull(b.Conn, h); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(h[:4]))
	head := int(binary.BigEndian.Uint16(h[4:6]))
	if head < 16 || size < head || size > biliMaxMessage {
		return nil, fmt.Errorf("无效 TCP 弹幕包长度 %d", size)
	}
	body := make([]byte, size-16)
	if _, err := io.ReadFull(b.Conn, body); err != nil {
		return nil, err
	}
	return append(h, body...), nil
}
func (b *biliTCP) WritePacket(p []byte) error {
	for len(p) > 0 {
		n, err := b.Conn.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

type biliWS struct {
	net.Conn
	input *bufio.Reader
	mu    sync.Mutex
}

func dialBiliWS(ctx context.Context, host string, port int, cookie string) (*biliWS, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	raw, err := (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 8 * time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	ws, err := upgradeBiliWS(raw, host, port, cookie)
	_ = raw.SetDeadline(time.Time{})
	if err != nil {
		raw.Close()
		return nil, err
	}
	return ws, nil
}

func upgradeBiliWS(raw net.Conn, host string, port int, cookie string) (*biliWS, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])
	h := sha1.Sum([]byte(key + wsGUID))
	expected := base64.StdEncoding.EncodeToString(h[:])
	request := fmt.Sprintf("GET /sub HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nOrigin: https://live.bilibili.com\r\nReferer: https://live.bilibili.com/\r\nUser-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36\r\n", net.JoinHostPort(host, fmt.Sprintf("%d", port)), key)
	// Cookie values originate in configuration and must never inject headers.
	if cookie != "" && !strings.ContainsAny(cookie, "\r\n") {
		request += "Cookie: " + cookie + "\r\n"
	}
	request += "\r\n"
	if _, err := io.WriteString(raw, request); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(raw, 8192)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") || !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Accept"), expected) || !strings.Contains(strings.ToLower(resp.Header.Get("Connection")), "upgrade") {
		resp.Body.Close()
		return nil, fmt.Errorf("B站 WSS 握手失败，HTTP %d", resp.StatusCode)
	}
	return &biliWS{Conn: raw, input: br}, nil
}

func (b *biliWS) WritePacket(p []byte) error { return b.writeFrame(2, p) }
func (b *biliWS) writeFrame(op byte, body []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(body) > biliMaxMessage {
		return errors.New("WebSocket 发送消息过大")
	}
	hdr := []byte{0x80 | op, 0x80}
	switch {
	case len(body) < 126:
		hdr[1] |= byte(len(body))
	case len(body) <= 0xffff:
		hdr[1] |= 126
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(len(body)))
	default:
		hdr[1] |= 127
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(len(body)))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	hdr = append(hdr, mask[:]...)
	masked := make([]byte, len(body))
	for i := range body {
		masked[i] = body[i] ^ mask[i%4]
	}
	frame := append(hdr, masked...)
	for len(frame) > 0 {
		n, err := b.Conn.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}
func (b *biliWS) ReadPacket() ([]byte, error) {
	var message []byte
	fragmented := false
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(b.input, h); err != nil {
			return nil, err
		}
		if h[0]&0x70 != 0 {
			return nil, errors.New("WebSocket RSV 标志无效")
		}
		fin, op := h[0]&0x80 != 0, h[0]&0xf
		if h[1]&0x80 != 0 {
			return nil, errors.New("WebSocket 服务端帧不应被掩码")
		}
		length := uint64(h[1] & 0x7f)
		if length == 126 {
			var n [2]byte
			if _, err := io.ReadFull(b.input, n[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(n[:]))
		}
		if length == 127 {
			var n [8]byte
			if _, err := io.ReadFull(b.input, n[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(n[:])
		}
		if length > uint64(biliMaxMessage)-uint64(len(message)) {
			return nil, errors.New("WebSocket 接收消息过大")
		}
		if op >= 8 && (!fin || length > 125) {
			return nil, errors.New("WebSocket 控制帧无效")
		}
		body := make([]byte, int(length))
		if _, err := io.ReadFull(b.input, body); err != nil {
			return nil, err
		}
		switch op {
		case 8:
			if len(body) >= 2 {
				code := binary.BigEndian.Uint16(body[:2])
				reason := string(body[2:])
				if len(reason) > 160 {
					reason = reason[:160]
				}
				// Close reason is untrusted; do not echo token/cookie or control chars.
				reason = strings.Map(func(r rune) rune {
					if r < 32 || r == 127 {
						return -1
					}
					return r
				}, reason)
				return nil, fmt.Errorf("WebSocket 服务端关闭：code=%d reason=%q", code, reason)
			}
			return nil, errors.New("WebSocket 服务端关闭（未提供关闭码）")
		case 9:
			if err := b.writeFrame(10, body); err != nil {
				return nil, err
			}
			continue
		case 10:
			continue
		case 2:
			if fragmented {
				return nil, errors.New("WebSocket 分片消息中出现新消息")
			}
			if fin {
				return body, nil
			}
			message = append(message, body...)
			fragmented = true
		case 0:
			if !fragmented {
				return nil, errors.New("WebSocket 意外续帧")
			}
			message = append(message, body...)
			if fin {
				return message, nil
			}
		default:
			return nil, fmt.Errorf("WebSocket 不支持的帧 opcode=%d", op)
		}
	}
}
