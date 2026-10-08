package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// The QR key and all session credentials are server-side only. These routes
// are intentionally admin-only, including the QR start and polling endpoints.
const (
	biliQRGenerateEndpoint = "https://passport.bilibili.com/x/passport-login/web/qrcode/generate"
	biliQRPollEndpoint     = "https://passport.bilibili.com/x/passport-login/web/qrcode/poll"
	biliNavEndpoint        = "https://api.bilibili.com/x/web-interface/nav"
)

type biliQRSession struct {
	Key    string
	Expiry time.Time
}

type biliQRResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		URL       string `json:"url"`
		QRCodeKey string `json:"qrcode_key"`
		Code      int    `json:"code"`
		Message   string `json:"message"`
	} `json:"data"`
}

type biliLoginResponse struct {
	Code int `json:"code"`
	Data struct {
		IsLogin bool   `json:"isLogin"`
		Mid     int64  `json:"mid"`
		Uname   string `json:"uname"`
	} `json:"data"`
}

func (a *App) biliClient() *http.Client {
	if a.qrClient != nil {
		return a.qrClient
	}
	return &http.Client{Timeout: 12 * time.Second}
}
func (a *App) biliJSON(ctx context.Context, endpoint, cookie string, result any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/135.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("Origin", "https://www.bilibili.com")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	response, err := a.biliClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("B站接口 HTTP %d", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(result); err != nil {
		return nil, errors.New("B站接口响应不是有效 JSON")
	}
	return response, nil
}
func trustedBiliURL(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "bilibili.com" || strings.HasSuffix(h, ".bilibili.com")
}

var cookieKeys = map[string]bool{"SESSDATA": true, "bili_jct": true, "DedeUserID": true, "DedeUserID__ckMd5": true, "sid": true, "buvid3": true, "buvid4": true}

func cookieFromQR(rawURL string, resp *http.Response) (string, error) {
	if !trustedBiliURL(rawURL) {
		return "", errors.New("B站扫码返回了不受信任的跳转地址")
	}
	parsed, _ := url.Parse(rawURL)
	values := map[string]string{}
	for k, arr := range parsed.Query() {
		if cookieKeys[k] && len(arr) > 0 && len(arr[0]) <= 4096 && !strings.ContainsAny(arr[0], "\r\n;") {
			values[k] = arr[0]
		}
	}
	for _, c := range resp.Cookies() {
		if cookieKeys[c.Name] && len(c.Value) <= 4096 && !strings.ContainsAny(c.Value, "\r\n;") {
			values[c.Name] = c.Value
		}
	}
	if values["SESSDATA"] == "" {
		return "", errors.New("B站未返回 SESSDATA 登录会话，请重新扫码")
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values[k])
	}
	return strings.Join(parts, "; "), nil
}

func (a *App) qrRoutes(mux *http.ServeMux) {
	start := func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		var resp biliQRResponse
		if _, err := a.biliJSON(ctx, a.qrGenerateURL, "", &resp); err != nil {
			send(w, 502, map[string]string{"error": err.Error()})
			return
		}
		if resp.Code != 0 || resp.Data.QRCodeKey == "" || !trustedBiliURL(resp.Data.URL) || len(resp.Data.QRCodeKey) > 256 {
			send(w, 502, map[string]string{"error": "B站没有返回有效的二维码登录地址"})
			return
		}
		a.qrMu.Lock()
		a.qrSession = biliQRSession{Key: resp.Data.QRCodeKey, Expiry: time.Now().Add(4 * time.Minute)}
		a.qrMu.Unlock()
		send(w, 200, map[string]any{"status": "waiting", "url": resp.Data.URL, "expires_in": 240})
	}
	mux.HandleFunc("POST /api/bili/qr/start", start)
	mux.HandleFunc("GET /api/bili/qr/start", start) // Original Python route compatibility.
	mux.HandleFunc("POST /api/bili/qr/poll", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		a.qrMu.Lock()
		defer a.qrMu.Unlock()
		key := a.qrSession.Key
		if key == "" || time.Now().After(a.qrSession.Expiry) {
			a.qrSession = biliQRSession{}
			send(w, 410, map[string]string{"error": "扫码会话已过期，请重新生成"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		endpoint := a.qrPollURL + "?" + url.Values{"qrcode_key": {key}}.Encode()
		var reply biliQRResponse
		resp, err := a.biliJSON(ctx, endpoint, "", &reply)
		if err != nil {
			send(w, 502, map[string]string{"error": err.Error()})
			return
		}
		if reply.Code != 0 {
			send(w, 502, map[string]string{"error": fmt.Sprintf("B站扫码接口错误 code=%d", reply.Code)})
			return
		}
		switch reply.Data.Code {
		case 86101:
			send(w, 200, map[string]string{"status": "waiting", "message": "等待扫码"})
			return
		case 86090:
			send(w, 200, map[string]string{"status": "scanned", "message": "已扫码，等待手机确认"})
			return
		case 86038:
			a.qrSession = biliQRSession{}
			send(w, 410, map[string]string{"error": "二维码已失效"})
			return
		case 0:
		default:
			send(w, 502, map[string]string{"error": fmt.Sprintf("B站扫码状态异常 code=%d", reply.Data.Code)})
			return
		}
		cookie, e := cookieFromQR(reply.Data.URL, resp)
		if e != nil {
			send(w, 502, map[string]string{"error": e.Error()})
			return
		}
		var nav biliLoginResponse
		_, e = a.biliJSON(ctx, a.qrNavURL, cookie, &nav)
		if e != nil || nav.Code != 0 || !nav.Data.IsLogin || nav.Data.Mid <= 0 {
			send(w, 502, map[string]string{"error": "B站扫码已确认，但登录状态验证失败；未保存会话"})
			return
		}
		a.mu.Lock()
		before := a.config.Bilibili.Cookie
		a.config.Bilibili.Cookie = cookie
		e = a.saveLocked()
		if e != nil {
			a.config.Bilibili.Cookie = before
		}
		cfg := a.config.Bilibili
		a.mu.Unlock()
		if e != nil {
			send(w, 500, map[string]string{"error": "本地保存登录状态失败"})
			return
		}
		a.qrSession = biliQRSession{}
		if cfg.Enabled {
			a.restartBilibili(cfg)
		}
		send(w, 200, map[string]any{"status": "success", "uid": nav.Data.Mid, "username": nav.Data.Uname, "message": "B站扫码登录成功，已安全保存会话"})
	})
	mux.HandleFunc("POST /api/bili/logout", func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		a.qrMu.Lock()
		a.qrSession = biliQRSession{}
		a.qrMu.Unlock()
		a.mu.Lock()
		before := a.config.Bilibili.Cookie
		a.config.Bilibili.Cookie = ""
		e := a.saveLocked()
		if e != nil {
			a.config.Bilibili.Cookie = before
		}
		cfg := a.config.Bilibili
		a.mu.Unlock()
		if e != nil {
			send(w, 500, map[string]string{"error": "保存失败"})
			return
		}
		if cfg.Enabled {
			a.restartBilibili(cfg)
		}
		send(w, 200, map[string]string{"status": "ok"})
	})
}

func (a *App) restartBilibili(pc PlatformConfig) {
	a.mu.Lock()
	if stop := a.workers["bilibili"]; stop != nil {
		stop()
		delete(a.workers, "bilibili")
	}
	if pc.Enabled && pc.Room != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.workers["bilibili"] = cancel
		go (live.Bilibili{}).Run(ctx, pc.Room, pc.Cookie, a.OnDanmu, a.publishStatus)
	}
	a.mu.Unlock()
}
