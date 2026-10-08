package live

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBiliCandidateOrdering(t *testing.T) {
	items := biliCandidates([]biliHost{{"a.chat.bilibili.com", 2243, 2245}, {"b.chat.bilibili.com", 2243, 2245}, {"a.chat.bilibili.com", 2243, 2245}})
	if len(items) != 4 || !items[0].wss || !items[1].wss || items[2].wss || items[3].wss {
		t.Fatalf("unexpected transports %+v", items)
	}
}

func TestBiliWBIAndCookie(t *testing.T) {
	k, err := wbiKey("https://i.test/abcdefghijklmnopqrstuvwxyz012345.png", "https://i.test/ABCDEFGHIJKLMNOPQRSTUVWXYZ012345.jpg")
	if err != nil || len(k) != 32 {
		t.Fatalf("wbi key %q %v", k, err)
	}
	params := url.Values{"id": {"12345"}, "type": {"0"}, "weird": {"a!b*c(d)"}}
	signed := wbiSign(params, k, time.Unix(1760000000, 0))
	if strings.Contains(signed.Get("weird"), "!") || strings.Contains(signed.Get("weird"), "*") || signed.Get("w_rid") == "" {
		t.Fatal(signed)
	}
	if biliCookieValue(" buvid3=abc123; SESSDATA=xxx", "buvid3") != "abc123" {
		t.Fatal("cookie")
	}
}

func TestBiliAuthNeedsPositiveACK(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{{"good", `{"code":0}`, false}, {"denied", `{"code":-101,"message":"not logged in"}`, true}, {"malformed", `{"code":`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := net.Pipe()
			defer c.Close()
			defer s.Close()
			stream := &biliTCP{Conn: c}
			go func() { _, _ = s.Write(biliPacket(8, []byte(tc.body))) }()
			err := readBiliAuth(stream)
			if (err != nil) != tc.wantErr {
				t.Fatalf("read auth=%v", err)
			}
		})
	}
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	go s.Close()
	if err := readBiliAuth(&biliTCP{Conn: c}); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestWSSUpgradeMaskingAndFragmentation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverErr := make(chan error, 1)
	go func() {
		br := bufio.NewReader(server)
		req, err := http.ReadRequest(br)
		if err != nil {
			serverErr <- err
			return
		}
		if req.URL.Path != "/sub" {
			serverErr <- fmt.Errorf("path %s", req.URL.Path)
			return
		}
		h := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + wsGUID))
		_, err = fmt.Fprintf(server, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(h[:]))
		if err != nil {
			serverErr <- err
			return
		}
		// Verify outbound binary frames are client-masked.
		fh := make([]byte, 2)
		if _, err = io.ReadFull(br, fh); err != nil {
			serverErr <- err
			return
		}
		if fh[0] != 0x82 || fh[1]&0x80 == 0 {
			serverErr <- fmt.Errorf("not masked %v", fh)
			return
		}
		n := int(fh[1] & 0x7f)
		mask := make([]byte, 4)
		io.ReadFull(br, mask)
		data := make([]byte, n)
		io.ReadFull(br, data)
		for i := range data {
			data[i] ^= mask[i%4]
		}
		if string(data) != "hello" {
			serverErr <- fmt.Errorf("sent payload %q", data)
			return
		}
		// Ping followed by a fragmented binary message.
		_, err = server.Write([]byte{0x89, 0x01, 'a', 0x02, 0x02, 'h', 'e', 0x80, 0x03, 'l', 'l', 'o'})
		if err != nil {
			serverErr <- err
			return
		}
		// Read the masked pong. The pong may be interleaved with the receiver.
		pong := make([]byte, 2)
		_, err = io.ReadFull(br, pong)
		if err != nil || pong[0] != 0x8a || pong[1]&0x80 == 0 {
			serverErr <- fmt.Errorf("pong %v %v", pong, err)
			return
		}
		mask = make([]byte, 4)
		io.ReadFull(br, mask)
		answer := make([]byte, 1)
		io.ReadFull(br, answer)
		answer[0] ^= mask[0]
		if answer[0] != 'a' {
			serverErr <- fmt.Errorf("pong data %v", answer)
			return
		}
		serverErr <- nil
	}()
	ws, err := upgradeBiliWS(client, "bili.test", 2245, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = ws.WritePacket([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	pkt, err := ws.ReadPacket()
	if err != nil || !bytes.Equal(pkt, []byte("hello")) {
		t.Fatalf("frame %q %v", pkt, err)
	}
	if err = <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestRunBiliStreamWaitsForAuthThenEmits(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan Event, 1)
	stat := make(chan Status, 2)
	result := make(chan error, 1)
	go func() {
		result <- runBiliStream(ctx, &biliTCP{Conn: client}, []byte(`{"uid":0}`), 123, "TCP", func(e Event) { got <- e }, func(s Status) { stat <- s })
	}()
	raw := make([]byte, 16)
	if _, err := io.ReadFull(server, raw); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(raw[8:12]) != 7 {
		t.Fatalf("expected op7, got %v", raw)
	}
	lenAuth := int(binary.BigEndian.Uint32(raw[:4])) - 16
	if _, err := io.CopyN(io.Discard, server, int64(lenAuth)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stat:
		t.Fatal("reported connected before auth ack")
	default:
	}
	if _, err := server.Write(biliPacket(8, []byte(`{"code":0}`))); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-stat:
		if !s.Connected {
			t.Fatal(s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no status after auth")
	}
	hb := make([]byte, 16)
	if _, err := io.ReadFull(server, hb); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(hb[8:12]) != 2 {
		t.Fatal("heartbeat missing")
	}
	if _, err := io.CopyN(io.Discard, server, int64(binary.BigEndian.Uint32(hb[:4])-16)); err != nil {
		t.Fatal(err)
	}
	chat := biliPacket(5, []byte(`{"cmd":"DANMU_MSG","info":[[],"排队",[34,"主播"]]}`))
	if _, err := server.Write(chat); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.Username != "主播" {
			t.Fatal(e)
		}
	case <-time.After(time.Second * 2):
		t.Fatal("no danmaku")
	}
	cancel()
	server.Close()
	select {
	case <-result:
	case <-time.After(time.Second * 2):
		t.Fatal("did not stop")
	}
}

func TestBiliDiscoverySignedPreferred(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var raw string
		switch req.URL.Path {
		case "/x/frontend/finger/spi":
			raw = `{"code":0,"data":{"b_3":"id123"}}`
		case "/room/v1/Room/room_init":
			raw = `{"code":0,"data":{"room_id":998,"uid":12345}}`
		case "/x/web-interface/nav":
			raw = `{"code":0,"data":{"wbi_img":{"img_url":"https://i/abcdefghijklmnopqrstuvwxyz012345.png","sub_url":"https://i/ABCDEFGHIJKLMNOPQRSTUVWXYZ012345.png"}}}`
		case "/xlive/web-room/v1/index/getDanmuInfo":
			if req.URL.Query().Get("w_rid") == "" || req.URL.Query().Get("wts") == "" {
				return nil, fmt.Errorf("signed URL missing")
			}
			if !strings.Contains(req.Header.Get("Cookie"), "buvid3=id123") {
				return nil, fmt.Errorf("buvid not attached")
			}
			raw = `{"code":0,"data":{"token":"opaque","host_list":[{"host":"test.chat.bilibili.com","wss_port":2245,"port":2243}]}}`
		default:
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header), Request: req}, nil
	})}
	b := Bilibili{Client: client}
	id, anchorUID, key, hosts, cookie, err := b.discover(context.Background(), "123", "")
	if err != nil || id != 998 || anchorUID != 12345 || key != "opaque" || len(hosts) != 1 || hosts[0].WSSPort != 2245 || !strings.Contains(cookie, "buvid3=id123") {
		t.Fatalf("%d %q %+v %v", id, key, hosts, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBiliAuthPayloadIncludesModernClientFields(t *testing.T) {
	body, err := biliAuthPayload(998, "temporary-token", "SESSDATA=x; buvid3=client-abc")
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(body, &values); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"uid": float64(0), "roomid": float64(998), "protover": float64(2), "buvid": "client-abc", "support_ack": true, "platform": "web", "type": float64(2), "key": "temporary-token"} {
		if values[key] != want {
			t.Fatalf("%s got %v want %v", key, values[key], want)
		}
	}
	if id, ok := values["queue_uuid"].(string); !ok || len(id) != 8 {
		t.Fatalf("queue_uuid %v", values["queue_uuid"])
	}
	if strings.Contains(string(body), "SESSDATA") {
		t.Fatal("auth leaked unrelated session cookie")
	}
}

func TestBiliRejectsUntrustedDanmuHost(t *testing.T) {
	out := biliCandidates([]biliHost{{Host: "evil.example", Port: 2243, WSSPort: 2245}, {Host: "a.chat.bilibili.com", Port: 2243, WSSPort: 2245}})
	if len(out) != 2 || out[0].host != "a.chat.bilibili.com" {
		t.Fatalf("unsafe server in candidate list %+v", out)
	}
}

func TestBiliWebSocketCloseExplainsCode(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	ws := &biliWS{Conn: left, input: bufio.NewReader(left)}
	go func() { _, _ = right.Write([]byte{0x88, 0x04, 0x03, 0xF0, 'n', 'o'}) }()
	_, err := ws.ReadPacket()
	if err == nil || !strings.Contains(err.Error(), "1008") || !strings.Contains(err.Error(), "no") {
		t.Fatalf("expected informative close reason, got %v", err)
	}
}

func TestBiliWSUpgradeSendsSessionCookie(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	result := make(chan error, 1)
	go func() {
		req, err := http.ReadRequest(bufio.NewReader(server))
		if err != nil {
			result <- err
			return
		}
		if req.Header.Get("Cookie") != "buvid3=client-123; SESSDATA=testing" {
			result <- fmt.Errorf("missing session cookie")
			return
		}
		if req.Header.Get("Origin") != "https://live.bilibili.com" {
			result <- fmt.Errorf("missing live origin")
			return
		}
		h := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + wsGUID))
		_, err = fmt.Fprintf(server, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(h[:]))
		result <- err
	}()
	if _, err := upgradeBiliWS(client, "a.chat.bilibili.com", 2245, "buvid3=client-123; SESSDATA=testing"); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
