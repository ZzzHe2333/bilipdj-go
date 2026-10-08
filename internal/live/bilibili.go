package live

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const biliAPI = "https://api.live.bilibili.com"

type Bilibili struct{ Client *http.Client }

type biliRoom struct {
	Code int `json:"code"`
	Data struct {
		RoomID int64 `json:"room_id"`
	} `json:"data"`
}
type biliHost struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	WSSPort int    `json:"wss_port"`
}
type biliConf struct {
	Code int `json:"code"`
	Data struct {
		Token    string     `json:"token"`
		Hosts    []biliHost `json:"host_server_list"`
		Fallback []biliHost `json:"host_list"`
	} `json:"data"`
}

func (b Bilibili) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: 12 * time.Second}
}
func biliGet[T any](ctx context.Context, c *http.Client, u, cookie string, dest *T) error {
	req, e := http.NewRequestWithContext(ctx, "GET", u, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://live.bilibili.com/")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, e := c.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Bilibili HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(dest)
}
func biliRoomID(input string) (int64, error) {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "/") {
		u, e := url.Parse(input)
		if e != nil {
			return 0, e
		}
		if u.Host != "live.bilibili.com" && u.Host != "www.live.bilibili.com" {
			return 0, fmt.Errorf("无效的 B站直播间地址")
		}
		input = strings.Split(strings.Trim(u.Path, "/"), "/")[0]
	}
	n, e := strconv.ParseInt(input, 10, 64)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("直播间号必须为正整数")
	}
	return n, nil
}
func (b Bilibili) discover(ctx context.Context, room, cookie string) (int64, string, []biliHost, error) {
	id, e := biliRoomID(room)
	if e != nil {
		return 0, "", nil, e
	}
	httpClient := b.client()
	cookie = b.discoveryCookie(ctx, cookie)
	var init biliRoom
	if e = biliGet(ctx, httpClient, fmt.Sprintf("%s/room/v1/Room/room_init?id=%d", biliAPI, id), cookie, &init); e != nil {
		return 0, "", nil, e
	}
	if init.Code != 0 || init.Data.RoomID <= 0 {
		return 0, "", nil, fmt.Errorf("B站直播间解析失败 code=%d", init.Code)
	}
	id = init.Data.RoomID
	var errorsSeen []string
	signedURL, signErr := b.signedDanmuURL(ctx, id, cookie)
	if signErr == nil {
		var conf biliConf
		e = biliGet(ctx, httpClient, signedURL, cookie, &conf)
		if e == nil && conf.Code == 0 && conf.Data.Token != "" && len(append(conf.Data.Hosts, conf.Data.Fallback...)) > 0 {
			hosts := conf.Data.Hosts
			if len(hosts) == 0 {
				hosts = conf.Data.Fallback
			}
			return id, conf.Data.Token, hosts, nil
		}
		errorsSeen = append(errorsSeen, fmt.Sprintf("signed getDanmuInfo code=%d err=%v", conf.Code, e))
	} else {
		errorsSeen = append(errorsSeen, "WBI: "+signErr.Error())
	}
	// Compatibility fallback for old API response formats or temporarily
	// unavailable WBI. Still require token and returned hosts, never guess.
	urls := []string{
		fmt.Sprintf("%s/room/v1/Danmu/getConf?room_id=%d&platform=pc&player=web", biliAPI, id),
		fmt.Sprintf("%s/xlive/web-room/v1/index/getDanmuInfo?id=%d&type=0", biliAPI, id),
	}
	for _, u := range urls {
		var conf biliConf
		e = biliGet(ctx, httpClient, u, cookie, &conf)
		if e == nil && conf.Code == 0 && conf.Data.Token != "" {
			hosts := conf.Data.Hosts
			if len(hosts) == 0 {
				hosts = conf.Data.Fallback
			}
			if len(hosts) > 0 {
				return id, conf.Data.Token, hosts, nil
			}
		}
		errorsSeen = append(errorsSeen, fmt.Sprintf("getDanmuInfo fallback code=%d err=%v", conf.Code, e))
	}
	return 0, "", nil, fmt.Errorf("B站认证 token/节点获取失败；请尝试填入有效的 B站 Cookie（含 buvid3）后重试；%s", strings.Join(errorsSeen, "; "))
}
func biliPacket(operation int, body []byte) []byte {
	p := make([]byte, 16+len(body))
	binary.BigEndian.PutUint32(p[:4], uint32(len(p)))
	binary.BigEndian.PutUint16(p[4:6], 16)
	binary.BigEndian.PutUint16(p[6:8], 1)
	binary.BigEndian.PutUint32(p[8:12], uint32(operation))
	binary.BigEndian.PutUint32(p[12:16], 1)
	copy(p[16:], body)
	return p
}
func biliWalk(data []byte, depth int, receive func([]byte)) error {
	if depth > 6 {
		return errors.New("弹幕压缩递归超过限制")
	}
	for len(data) >= 16 {
		size := int(binary.BigEndian.Uint32(data[:4]))
		h := int(binary.BigEndian.Uint16(data[4:6]))
		ver := binary.BigEndian.Uint16(data[6:8])
		op := binary.BigEndian.Uint32(data[8:12])
		if h < 16 || size < h || size > len(data) || size > 8<<20 {
			return errors.New("无效的 B站弹幕包长度")
		}
		body := data[h:size]
		data = data[size:]
		if op != 5 {
			continue
		}
		switch ver {
		case 0, 1:
			receive(body)
		case 2:
			zr, e := zlib.NewReader(bytes.NewReader(body))
			if e != nil {
				return e
			}
			decoded, e := io.ReadAll(io.LimitReader(zr, 8<<20))
			zr.Close()
			if e != nil {
				return e
			}
			if len(decoded) >= 8<<20 {
				return errors.New("弹幕解压数据过大")
			}
			if e = biliWalk(decoded, depth+1, receive); e != nil {
				return e
			}
		default:
			return fmt.Errorf("未知的弹幕压缩版本 %d", ver)
		}
	}
	return nil
}
func biliMessage(data []byte) (Event, bool) {
	var x struct {
		Cmd  string            `json:"cmd"`
		Info []json.RawMessage `json:"info"`
	}
	if json.Unmarshal(data, &x) != nil || !strings.HasPrefix(x.Cmd, "DANMU_MSG") || len(x.Info) < 3 {
		return Event{}, false
	}
	var msg string
	_ = json.Unmarshal(x.Info[1], &msg)
	var user []json.RawMessage
	if json.Unmarshal(x.Info[2], &user) != nil || len(user) < 2 {
		return Event{}, false
	}
	var name string
	var id json.Number
	_ = json.Unmarshal(user[0], &id)
	_ = json.Unmarshal(user[1], &name)
	if msg == "" || name == "" {
		return Event{}, false
	}
	return Event{Platform: "bilibili", UserID: id.String(), Username: name, Content: msg, Time: time.Now()}, true
}

// biliCandidate chooses encrypted WebSocket first: Bilibili currently exposes
// /sub as the stable browser transport. Raw TCP remains a compatibility fallback.
type biliCandidate struct {
	host string
	port int
	wss  bool
}

func biliCandidates(hosts []biliHost) []biliCandidate {
	out := make([]biliCandidate, 0, len(hosts)*2)
	seen := map[string]bool{}
	for _, ws := range []bool{true, false} {
		for _, h := range hosts {
			port := h.Port
			if ws {
				port = h.WSSPort
			}
			if h.Host == "" || port <= 0 {
				continue
			}
			id := fmt.Sprintf("%s:%d/%t", h.Host, port, ws)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, biliCandidate{h.Host, port, ws})
		}
	}
	return out
}

func dialBili(ctx context.Context, item biliCandidate) (biliStream, error) {
	if item.wss {
		return dialBiliWS(ctx, item.host, item.port)
	}
	c, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(item.host, strconv.Itoa(item.port)))
	if err != nil {
		return nil, err
	}
	return &biliTCP{Conn: c}, nil
}

func readBiliAuth(stream biliStream) error {
	// Don't report connected until Bilibili replies with operation 8, code=0.
	// Some servers close the socket after rejecting authentication; previously
	// that appeared only as a mysterious EOF.
	for i := 0; i < 5; i++ {
		frame, err := stream.ReadPacket()
		if err != nil {
			return fmt.Errorf("等待鉴权结果失败: %w", err)
		}
		for len(frame) >= 16 {
			size := int(binary.BigEndian.Uint32(frame[:4]))
			head := int(binary.BigEndian.Uint16(frame[4:6]))
			if head < 16 || size < head || size > len(frame) {
				return errors.New("无效的鉴权响应包")
			}
			if binary.BigEndian.Uint32(frame[8:12]) == 8 {
				var result struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal(frame[head:size], &result); err != nil {
					return fmt.Errorf("鉴权结果解析失败: %w", err)
				}
				if result.Code != 0 {
					return fmt.Errorf("B站鉴权拒绝 code=%d %s", result.Code, result.Message)
				}
				return nil
			}
			frame = frame[size:]
		}
	}
	return errors.New("B站未发送鉴权确认包")
}

func (b Bilibili) connect(ctx context.Context, room, cookie string, emit Emit, report Report) error {
	report(Status{Platform: "bilibili", Connected: false, Message: "正在发现 B站弹幕服务器…", Since: time.Now()})
	id, token, hosts, err := b.discover(ctx, room, cookie)
	if err != nil {
		return err
	}
	var failures []string
	for _, item := range biliCandidates(hosts) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		transport := "TCP"
		if item.wss {
			transport = "WSS"
		}
		report(Status{Platform: "bilibili", Connected: false, Message: fmt.Sprintf("正在连接 B站 %s 节点 %s:%d", transport, item.host, item.port), Since: time.Now()})
		stream, e := dialBili(ctx, item)
		if e != nil {
			failures = append(failures, fmt.Sprintf("%s %s:%d: %v", transport, item.host, item.port, e))
			continue
		}
		auth, _ := json.Marshal(map[string]any{"uid": 0, "roomid": id, "protover": 2, "platform": "web", "type": 2, "key": token})
		e = runBiliStream(ctx, stream, auth, id, transport, emit, report)
		stream.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s %s:%d: %v", transport, item.host, item.port, e))
		// Once authenticated and streaming, disconnection should trigger fresh
		// discovery (possibly new token/hosts), not another stale-token dial.
		var active streamActiveError
		if errors.As(e, &active) {
			return e
		}
	}
	if len(failures) > 4 {
		failures = failures[:4]
	}
	return fmt.Errorf("B站所有弹幕节点连接/鉴权失败: %s", strings.Join(failures, "; "))
}

type streamActiveError struct{ err error }

func (e streamActiveError) Error() string { return e.err.Error() }
func (e streamActiveError) Unwrap() error { return e.err }

func runBiliStream(ctx context.Context, stream biliStream, auth []byte, roomID int64, transport string, emit Emit, report Report) error {
	if err := stream.WritePacket(biliPacket(7, auth)); err != nil {
		return fmt.Errorf("发送 B站鉴权包失败: %w", err)
	}
	// One socket, two serialized packet writers: heartbeat and WS ping/pong.
	// Read deadline applies to the whole frame: no short 3s read that can
	// discard a partially delivered TCP header.
	var conn net.Conn
	switch s := stream.(type) {
	case *biliWS:
		conn = s.Conn
	case *biliTCP:
		conn = s.Conn
	}
	if conn == nil {
		return errors.New("未知 B站传输")
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := readBiliAuth(stream); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	report(Status{Platform: "bilibili", Connected: true, Message: fmt.Sprintf("已通过 %s 鉴权，直播间 %d", transport, roomID), Since: time.Now()})
	var writer sync.Mutex
	write := func(p []byte) error {
		writer.Lock()
		defer writer.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return stream.WritePacket(p)
	}
	if err := write(biliPacket(2, []byte("[object Object]"))); err != nil {
		return streamActiveError{err}
	}
	heartbeatErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := write(biliPacket(2, []byte("[object Object]"))); err != nil {
					select {
					case heartbeatErr <- err:
					default:
					}
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(95 * time.Second))
		data, err := stream.ReadPacket()
		if err != nil {
			select {
			case he := <-heartbeatErr:
				return streamActiveError{fmt.Errorf("B站心跳失败: %w", he)}
			default:
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return streamActiveError{fmt.Errorf("B站 %s 读取中断: %w", transport, err)}
		}
		// Ignore non-chat events (including heartbeat replies); compressed
		// operation 5 packets are handled recursively by biliWalk.
		err = biliWalk(data, 0, func(raw []byte) {
			if event, ok := biliMessage(raw); ok {
				emit(event)
			}
		})
		if err != nil {
			return streamActiveError{fmt.Errorf("弹幕包解析失败: %w", err)}
		}
	}
	return ctx.Err()
}
func (b Bilibili) Run(ctx context.Context, room, cookie string, emit Emit, report Report) {
	for ctx.Err() == nil {
		e := b.connect(ctx, room, cookie, emit, report)
		if ctx.Err() != nil {
			break
		}
		report(Status{Platform: "bilibili", Connected: false, Message: fmt.Sprintf("连接中断：%v，5秒后重试", e), Since: time.Now()})
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	report(Status{Platform: "bilibili", Connected: false, Message: "已停止", Since: time.Now()})
}
func ValidateBilibiliRoom(room string) (int64, error) { return biliRoomID(room) }
