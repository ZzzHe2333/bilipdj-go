package core

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testReadWSFrame(r *bufio.Reader) (byte, []byte, error) {
	h := make([]byte, 2)
	if _, e := io.ReadFull(r, h); e != nil {
		return 0, nil, e
	}
	n := int(h[1] & 127)
	if n == 126 {
		var b [2]byte
		if _, e := io.ReadFull(r, b[:]); e != nil {
			return 0, nil, e
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	}
	if n > 4096 {
		return 0, nil, fmt.Errorf("unexpected frame len %d", n)
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return h[0] & 15, b, e
}
func TestLegacyWebSocketReadOnlyBridge(t *testing.T) {
	a := New(t.TempDir(), "0.6.0", "repo")
	a.queue = []QueueItem{{Key: "alice", Platform: "bilibili", UserID: "1", Username: "Alice", At: time.Now()}}
	srv := httptest.NewServer(a.Routes(http.NotFoundHandler()))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	conn, e := net.Dial("tcp", u.Host)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.Host, key)
	br := bufio.NewReader(conn)
	line, e := br.ReadString('\n')
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(line, "101") {
		t.Fatalf("handshake failed: %s", line)
	}
	for {
		s, e := br.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if s == "\r\n" {
			break
		}
	}
	for _, want := range []string{"PDJ_STATUS", "QUEUE_UPDATE"} {
		op, b, e := testReadWSFrame(br)
		if e != nil {
			t.Fatal(e)
		}
		if op != 1 {
			t.Fatalf("opcode %d", op)
		}
		var obj map[string]any
		_ = json.Unmarshal(b, &obj)
		if obj["type"] != want {
			t.Fatalf("got %s want %s", b, want)
		}
	}
	// Masked client text frames are accepted but cannot execute queue mutations.
	sendMasked := func(op byte, b []byte) {
		h := []byte{0x80 | op, byte(0x80 | len(b)), 1, 2, 3, 4}
		for i := range b {
			h = append(h, b[i]^h[2+i%4])
		}
		_, _ = conn.Write(h)
	}
	sendMasked(1, []byte("clear"))
	a.mu.RLock()
	initialLen := len(a.queue)
	a.mu.RUnlock()
	if initialLen != 1 {
		t.Fatal("client command mutated queue")
	}
	a.OnDanmu(live.Event{Platform: "douyin", UserID: "2", Username: "Bob", Content: "排队", Time: time.Now()})
	sawQueue := false
	sawDouyin := false
	for i := 0; i < 3; i++ {
		_, b, e := testReadWSFrame(br)
		if e != nil {
			t.Fatal(e)
		}
		var obj map[string]any
		_ = json.Unmarshal(b, &obj)
		if obj["type"] == "QUEUE_UPDATE" {
			sawQueue = true
			if len(obj["queue"].([]any)) != 2 {
				t.Errorf("queue update %s", b)
			}
		}
		if obj["type"] == "DOUYIN_DANMU" {
			sawDouyin = true
		}
		if sawQueue && sawDouyin {
			break
		}
	}
	if !sawQueue || !sawDouyin {
		t.Fatalf("bridge missing queue/douyin event queue=%t douyin=%t", sawQueue, sawDouyin)
	}
	sendMasked(9, []byte("ok"))
	op, b, e := testReadWSFrame(br)
	if e != nil || op != 10 || string(b) != "ok" {
		t.Fatalf("ping/pong: %d %q %v", op, b, e)
	}
	sendMasked(8, []byte{})
}
func TestLegacyWSRejectBadHandshake(t *testing.T) {
	a := New(t.TempDir(), "0.6.0", "repo")
	s := httptest.NewServer(a.Routes(http.NotFoundHandler()))
	defer s.Close()
	r, _ := http.NewRequest("GET", s.URL+"/ws", nil)
	r.Header.Set("Origin", "https://evil.example")
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("origin bypass: %d", resp.StatusCode)
	}
}
