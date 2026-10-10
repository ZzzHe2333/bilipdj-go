package live

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Douyin uses the same public webcast/im/fetch protobuf endpoint as the Python edition.
// The narrow wire decoder intentionally avoids pulling in the entire generated schema.
type Douyin struct{ Client *http.Client }

func (d Douyin) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

const douyinUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

var (
	douyinRoom   = regexp.MustCompile(`(?:\\?"(?:roomId|room_id|web_rid)\\?"\s*:\s*\\?")(\d+)`)
	douyinUnique = regexp.MustCompile(`(?:\\?"(?:user_unique_id|userUniqueId)\\?"\s*:\s*\\?")(\d+)`)
	douyinPath   = regexp.MustCompile(`^[0-9A-Za-z_-]{1,100}$`)
	douyinSharedLink = regexp.MustCompile(`https?://[^\s"<>，。]+`)
)

type douyinInfo struct{ LiveID, RoomID, UniqueID, TTWID string }

func douyinLiveID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		// Pasted share text can include a URL surrounded by prose.
		match := douyinSharedLink.FindString(s)
		if match == "" { return "", errors.New("请输入房间号或 live.douyin.com 链接") }
		u, e := url.Parse(strings.TrimRight(match, "；;、)]）"))
		if e != nil {
			return "", e
		}
		if u.Hostname() != "live.douyin.com" || u.Port() != "" || u.User != nil {
			return "", errors.New("仅接受 live.douyin.com 直播链接")
		}
		s = strings.Trim(u.Path, "/")
		s = strings.Split(s, "/")[0]
	}
	if !douyinPath.MatchString(s) {
		return "", errors.New("无效的抖音 live_id")
	}
	return s, nil
}
func douyinHeaders(req *http.Request, cookie string) {
	req.Header.Set("User-Agent", douyinUA)
	req.Header.Set("Referer", "https://live.douyin.com/")
	req.Header.Set("Origin", "https://live.douyin.com")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}
func cookieValue(cookie, key string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}
func randomValue() string {
	b := make([]byte, 60)
	if _, e := rand.Read(b); e != nil {
		return "randomfallback"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (d Douyin) bootstrap(ctx context.Context, liveID, cookie string) (douyinInfo, error) {
	id, e := douyinLiveID(liveID)
	if e != nil {
		return douyinInfo{}, e
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://live.douyin.com/"+url.PathEscape(id), nil)
	douyinHeaders(req, cookie)
	resp, e := d.client().Do(req)
	if e != nil {
		return douyinInfo{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return douyinInfo{}, fmt.Errorf("抖音房间页 HTTP %d", resp.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if e != nil {
		return douyinInfo{}, e
	}
	raw := string(body)
	room := douyinRoom.FindStringSubmatch(raw)
	uid := douyinUnique.FindStringSubmatch(raw)
	if len(room) < 2 || len(uid) < 2 {
		return douyinInfo{}, errors.New("抖音页面无法解析 roomId/user_unique_id：可能需要 Cookie，或者页面协议已变化")
	}
	ttwid := cookieValue(cookie, "ttwid")
	if ttwid == "" {
		for _, sc := range resp.Header.Values("Set-Cookie") {
			if strings.HasPrefix(sc, "ttwid=") {
				ttwid = strings.SplitN(strings.TrimPrefix(sc, "ttwid="), ";", 2)[0]
				break
			}
		}
	}
	return douyinInfo{LiveID: id, RoomID: room[1], UniqueID: uid[1], TTWID: ttwid}, nil
}
func (d Douyin) poll(ctx context.Context, info douyinInfo, cookie, msToken, bogus, cursor, internalExt string) (pollResult, error) {
	q := url.Values{}
	for k, v := range map[string]string{
		"resp_content_type": "protobuf", "did_rule": "3", "device_id": "", "app_name": "douyin_web", "endpoint": "live_pc",
		"support_wrds": "1", "user_unique_id": info.UniqueID, "identity": "audience", "need_persist_msg_count": "15",
		"insert_task_id": "", "live_reason": "", "room_id": info.RoomID, "version_code": "180800", "last_rtt": "0",
		"live_id": "1", "aid": "6383", "fetch_rule": "1", "cursor": cursor, "internal_ext": internalExt,
		"device_platform": "web", "cookie_enabled": "true", "screen_width": "1920", "screen_height": "1080",
		"browser_language": "zh-CN", "browser_platform": "Win32", "browser_name": "Mozilla", "browser_version": douyinUA,
		"browser_online": "true", "tz_name": "Asia/Shanghai", "msToken": msToken, "a_bogus": bogus,
	} {
		q.Set(k, v)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://live.douyin.com/webcast/im/fetch/?"+q.Encode(), nil)
	douyinHeaders(req, cookie)
	req.Header.Set("Referer", "https://live.douyin.com/"+url.PathEscape(info.LiveID))
	if info.TTWID != "" && cookieValue(cookie, "ttwid") == "" {
		req.Header.Set("Cookie", strings.TrimSpace(cookie+"; ttwid="+info.TTWID))
	}
	resp, e := d.client().Do(req)
	if e != nil {
		return pollResult{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return pollResult{}, fmt.Errorf("抖音弹幕拉取 HTTP %d", resp.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if e != nil {
		return pollResult{}, e
	}
	if len(data) >= 4<<20 {
		return pollResult{}, errors.New("抖音数据包超过上限")
	}
	result, e := parseDouyinResponse(data)
	if e != nil {
		return pollResult{}, e
	}
	if result.Cursor == "" {
		result.Cursor = cursor
	}
	if result.Ext == "" {
		result.Ext = internalExt
	}
	return result, nil
}
func (d Douyin) connect(ctx context.Context, room, cookie string, emit Emit, report Report) error {
	info, e := d.bootstrap(ctx, room, cookie)
	if e != nil {
		return e
	}
	report(Status{Platform: "douyin", Connected: true, Message: "已开始拉取直播间 " + info.LiveID, Since: time.Now()})
	cursor, ext := "", ""
	msToken := cookieValue(cookie, "msToken")
	if msToken == "" {
		msToken = randomValue()
	}
	bogus := randomValue()[:8]
	for ctx.Err() == nil {
		result, e := d.poll(ctx, info, cookie, msToken, bogus, cursor, ext)
		if e != nil {
			return e
		}
		cursor = result.Cursor
		ext = result.Ext
		for _, event := range result.Events {
			emit(event)
		}
		delay := result.Interval
		if delay < 200*time.Millisecond {
			delay = time.Second
		}
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return ctx.Err()
}
func (d Douyin) Run(ctx context.Context, room, cookie string, emit Emit, report Report) {
	for ctx.Err() == nil {
		e := d.connect(ctx, room, cookie, emit, report)
		if ctx.Err() != nil {
			break
		}
		report(Status{Platform: "douyin", Connected: false, Message: fmt.Sprintf("拉取失败：%v，5秒后重试", e), Since: time.Now()})
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	report(Status{Platform: "douyin", Connected: false, Message: "已停止", Since: time.Now()})
}
func ValidateDouyinRoom(room string) (string, error) { return douyinLiveID(room) }
