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
	Host string `json:"host"`
	Port int    `json:"port"`
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
	var init biliRoom
	if e = biliGet(ctx, b.client(), fmt.Sprintf("%s/room/v1/Room/room_init?id=%d", biliAPI, id), cookie, &init); e != nil {
		return 0, "", nil, e
	}
	if init.Code != 0 || init.Data.RoomID <= 0 {
		return 0, "", nil, fmt.Errorf("B站直播间解析失败 code=%d", init.Code)
	}
	id = init.Data.RoomID
	var conf biliConf
	u := fmt.Sprintf("%s/room/v1/Danmu/getConf?room_id=%d&platform=pc&player=web", biliAPI, id)
	e = biliGet(ctx, b.client(), u, cookie, &conf)
	if e != nil || conf.Code != 0 || conf.Data.Token == "" || len(conf.Data.Hosts) == 0 {
		conf = biliConf{}
		e = biliGet(ctx, b.client(), fmt.Sprintf("%s/xlive/web-room/v1/index/getDanmuInfo?id=%d&type=0", biliAPI, id), cookie, &conf)
	}
	if e != nil {
		return 0, "", nil, e
	}
	if conf.Code != 0 || conf.Data.Token == "" {
		return 0, "", nil, errors.New("B站弹幕服务器发现失败")
	}
	hosts := conf.Data.Hosts
	if len(hosts) == 0 {
		hosts = conf.Data.Fallback
	}
	if len(hosts) == 0 {
		return 0, "", nil, errors.New("B站未返回 TCP 弹幕服务器")
	}
	return id, conf.Data.Token, hosts, nil
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
func (b Bilibili) connect(ctx context.Context, room, cookie string, emit Emit, report Report) error {
	id, token, hosts, e := b.discover(ctx, room, cookie)
	if e != nil {
		return e
	}
	var c net.Conn
	for _, h := range hosts {
		if h.Port <= 0 || h.Host == "" {
			continue
		}
		dialer := net.Dialer{Timeout: 8 * time.Second}
		c, e = dialer.DialContext(ctx, "tcp", net.JoinHostPort(h.Host, strconv.Itoa(h.Port)))
		if e == nil {
			break
		}
	}
	if c == nil {
		return fmt.Errorf("B站 TCP 连接失败: %w", e)
	}
	defer c.Close()
	auth, _ := json.Marshal(map[string]any{"uid": 0, "roomid": id, "protover": 2, "platform": "web", "type": 2, "key": token})
	if _, e = c.Write(biliPacket(7, auth)); e != nil {
		return e
	}
	report(Status{Platform: "bilibili", Connected: true, Message: fmt.Sprintf("已连接直播间 %d", id), Since: time.Now()})
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-stop:
		}
	}()
	heartbeat := time.Now().Add(-time.Second)
	for ctx.Err() == nil {
		if time.Now().After(heartbeat) {
			if _, e = c.Write(biliPacket(2, []byte("[object Object]"))); e != nil {
				return e
			}
			heartbeat = time.Now().Add(30 * time.Second)
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		hdr := make([]byte, 16)
		_, e = io.ReadFull(c, hdr)
		if ne, ok := e.(net.Error); ok && ne.Timeout() {
			continue
		}
		if e != nil {
			return e
		}
		size := int(binary.BigEndian.Uint32(hdr[:4]))
		if size < 16 || size > 8<<20 {
			return errors.New("异常的弹幕帧长度")
		}
		body := make([]byte, size-16)
		_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
		_, e = io.ReadFull(c, body)
		if e != nil {
			return e
		}
		all := append(hdr, body...)
		_ = biliWalk(all, 0, func(raw []byte) {
			if event, ok := biliMessage(raw); ok {
				emit(event)
			}
		})
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
