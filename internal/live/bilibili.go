package live

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
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
		UID    int64 `json:"uid"`
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

// biliDiscovery keeps the auth UID, token and cookie obtained from the same
// browser session. A token fetched for a logged-in account must not be sent
// back to comet with the anonymous UID=0.
type biliDiscovery struct {
	roomID, anchorUID, authUID int64
	token, cookie              string
	hosts                      []biliHost
	loginConfirmed             bool
}

func biliCookieUID(cookie string) int64 {
	if biliCookieValue(cookie, "SESSDATA") == "" {
		return 0
	}
	n, err := strconv.ParseInt(biliCookieValue(cookie, "DedeUserID"), 10, 64)
	if err != nil || n < 1 || n > (1<<53)-1 {
		return 0
	}
	return n
}

func (b Bilibili) discover(ctx context.Context, room, cookie string) (biliDiscovery, error) {
	var out biliDiscovery
	id, e := biliRoomID(room)
	if e != nil {
		return out, e
	}
	client := b.client()
	cookie = b.discoveryCookie(ctx, cookie)
	out.cookie = cookie
	var init biliRoom
	if e = biliGet(ctx, client, fmt.Sprintf("%s/room/v1/Room/room_init?id=%d", biliAPI, id), cookie, &init); e != nil {
		return out, e
	}
	if init.Code != 0 || init.Data.RoomID <= 0 {
		return out, fmt.Errorf("B站直播间解析失败 code=%d", init.Code)
	}
	out.roomID, out.anchorUID = init.Data.RoomID, init.Data.UID
	out.authUID = biliCookieUID(cookie)
	var errorsSeen []string
	signedURL, nav, signErr := b.signedDanmuURL(ctx, out.roomID, cookie)
	if signErr == nil {
		// Prefer authoritative nav identity. A valid nav response saying the user
		// is logged out takes precedence over an outdated DedeUserID cookie.
		if nav.Data.IsLogin && nav.Data.Mid > 0 {
			out.authUID = nav.Data.Mid
			out.loginConfirmed = true
		} else {
			out.authUID = 0
		}
		var conf biliConf
		e = biliGet(ctx, client, signedURL, cookie, &conf)
		if e == nil && conf.Code == 0 && conf.Data.Token != "" && len(append(conf.Data.Hosts, conf.Data.Fallback...)) > 0 {
			out.token, out.hosts = conf.Data.Token, conf.Data.Hosts
			if len(out.hosts) == 0 {
				out.hosts = conf.Data.Fallback
			}
			return out, nil
		}
		errorsSeen = append(errorsSeen, fmt.Sprintf("signed getDanmuInfo code=%d err=%v", conf.Code, e))
	} else {
		errorsSeen = append(errorsSeen, "WBI: "+signErr.Error())
	}
	// Legacy token discovery fallback; preserve UID only when cookie/SESSION
	// indicates a plausible authenticated user and nav was not available.
	for _, endpoint := range []string{
		fmt.Sprintf("%s/room/v1/Danmu/getConf?room_id=%d&platform=pc&player=web", biliAPI, out.roomID),
		fmt.Sprintf("%s/xlive/web-room/v1/index/getDanmuInfo?id=%d&type=0", biliAPI, out.roomID),
	} {
		var conf biliConf
		e = biliGet(ctx, client, endpoint, cookie, &conf)
		if e == nil && conf.Code == 0 && conf.Data.Token != "" {
			out.token, out.hosts = conf.Data.Token, conf.Data.Hosts
			if len(out.hosts) == 0 {
				out.hosts = conf.Data.Fallback
			}
			if len(out.hosts) > 0 {
				return out, nil
			}
		}
		errorsSeen = append(errorsSeen, fmt.Sprintf("fallback code=%d err=%v", conf.Code, e))
	}
	return out, fmt.Errorf("B站认证 token/节点获取失败: %s", strings.Join(errorsSeen, "; "))
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
	isRoomAdmin := false
	var role int
	if len(user) > 2 {
		_ = json.Unmarshal(user[2], &role)
		isRoomAdmin = role == 1
	}
	guardLevel := 0
	if len(x.Info) > 3 {
		var medal []json.RawMessage
		if json.Unmarshal(x.Info[3], &medal) == nil && len(medal) > 10 {
			_ = json.Unmarshal(medal[10], &guardLevel)
		}
	}
	return Event{Platform: "bilibili", UserID: id.String(), Username: name, Content: msg, IsRoomAdmin: isRoomAdmin, GuardLevel: guardLevel, Time: time.Now()}, true
}

// biliGiftMessage recognizes only live-server gift commands (no client-side
// imitation or trust in claimed price/coin fields for battery threshold).
func biliGiftMessage(raw []byte) (Event, bool) {
	var frame struct {
		Cmd  string `json:"cmd"`
		Data struct {
			UID      json.Number     `json:"uid"`
			Username string          `json:"uname"`
			AltName  string          `json:"username"`
			Name     string          `json:"giftName"`
			AltGift  string          `json:"gift_name"`
			GiftID   int64           `json:"giftId"`
			Count    int             `json:"num"`
			CoinType string          `json:"coin_type"`
			Tid      string          `json:"tid"`
			Rnd      json.RawMessage `json:"rnd"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&frame) != nil {
		return Event{}, false
	}
	cmd := strings.SplitN(frame.Cmd, ":", 2)[0]
	if cmd != "SEND_GIFT" && cmd != "GUARD_BUY" {
		return Event{}, false
	}
	id := frame.Data.UID.String()
	if _, err := strconv.ParseUint(id, 10, 64); err != nil || id == "0" {
		return Event{}, false
	}
	name := strings.TrimSpace(frame.Data.Username)
	if name == "" {
		name = strings.TrimSpace(frame.Data.AltName)
	}
	if name == "" {
		return Event{}, false
	}
	giftName := strings.TrimSpace(frame.Data.Name)
	if giftName == "" {
		giftName = strings.TrimSpace(frame.Data.AltGift)
	}
	if cmd == "GUARD_BUY" && giftName == "" {
		giftName = "大航海"
	}
	count := frame.Data.Count
	if count <= 0 {
		count = 1
	}
	if count > 10000 {
		count = 10000
	}
	eventID := strings.TrimSpace(frame.Data.Tid)
	if eventID == "" && len(frame.Data.Rnd) > 0 {
		eventID = strings.Trim(string(frame.Data.Rnd), "\"")
	}
	if len(eventID) > 120 {
		eventID = ""
	}
	return Event{Kind: "gift", Platform: "bilibili", UserID: id, Username: name, Time: time.Now(),
		Gift: &Gift{EventID: eventID, Name: giftName, Count: count, CoinType: frame.Data.CoinType,
			GiftID: frame.Data.GiftID, GuardBuy: cmd == "GUARD_BUY"}}, true
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
			if !biliTrustedHost(h.Host) || port <= 0 || port > 65535 {
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

func biliTrustedHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.HasSuffix(host, ".chat.bilibili.com") && !strings.ContainsAny(host, "/:@\r\n ")
}

func biliAuthPayload(roomID, authUID int64, token, cookie string) ([]byte, error) {
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"uid": authUID, "roomid": roomID, "protover": 2,
		"buvid":       biliCookieValue(cookie, "buvid3"),
		"support_ack": true,
		"queue_uuid":  hex.EncodeToString(nonce[:]), "scene": "",
		"platform": "web", "type": 2, "key": token,
	})
}

func dialBili(ctx context.Context, item biliCandidate, cookie string) (biliStream, error) {
	if item.wss {
		return dialBiliWS(ctx, item.host, item.port, cookie)
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
	discovery, err := b.discover(ctx, room, cookie)
	if err != nil {
		return err
	}
	var failures []string
	for _, item := range biliCandidates(discovery.hosts) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		transport := "TCP"
		if item.wss {
			transport = "WSS"
		}
		report(Status{Platform: "bilibili", Connected: false, Message: fmt.Sprintf("正在连接 B站 %s 节点 %s:%d", transport, item.host, item.port), Since: time.Now()})
		stream, e := dialBili(ctx, item, discovery.cookie)
		if e != nil {
			failures = append(failures, fmt.Sprintf("%s %s:%d: %v", transport, item.host, item.port, e))
			continue
		}
		auth, e := biliAuthPayload(discovery.roomID, discovery.authUID, discovery.token, discovery.cookie)
		if e != nil {
			stream.Close()
			return e
		}
		e = runBiliStream(ctx, stream, auth, discovery.roomID, transport, func(event Event) {
			// Anchor identity is determined by authoritative room_init UID.
			if discovery.anchorUID > 0 && event.UserID == strconv.FormatInt(discovery.anchorUID, 10) {
				event.IsAnchor = true
			}
			emit(event)
		}, report)
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
	// Report at least one failure from each transport, rather than hiding
	// every TCP attempt behind the four WSS attempts.
	if len(failures) > 8 {
		failures = append(failures[:4], append([]string{"…其余节点省略…"}, failures[len(failures)-3:]...)...)
	}
	loginNote := ""
	if !discovery.loginConfirmed {
		if biliCookieValue(discovery.cookie, "SESSDATA") == "" {
			loginNote = "（未配置 B站登录会话：可先在平台设置中填写自己账号的有效 Cookie，再重试；不要分享 Cookie）"
		} else {
			loginNote = "（B站 nav 未确认登录态：请检查 Cookie 是否过期、SESSDATA / DedeUserID 是否一致）"
		}
	}
	return fmt.Errorf("B站所有弹幕节点连接/鉴权失败%s: %s", loginNote, strings.Join(failures, "; "))
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
			} else if gift, ok := biliGiftMessage(raw); ok {
				emit(gift)
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
