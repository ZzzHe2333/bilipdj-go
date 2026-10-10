package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	TargetID string
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

// trustedBiliURL validates the QR image URL; it is not used to authorize
// network requests to the cross-domain login callback.
func trustedBiliURL(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "bilibili.com" || strings.HasSuffix(h, ".bilibili.com")
}

// Bilibili's successful QR poll returns a biligame crossDomain link, not the
// passport.bilibili.com QR image link. Only these exact HTTPS callback origins
// and paths may trigger a server-side HTTP request; the ticket is a secret.
func trustedBiliCrossDomain(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Opaque != "" || u.Fragment != "" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h != "passport.biligame.com" && h != "passport.bilibili.com" {
		return false
	}
	return u.EscapedPath() == "/crossDomain" || u.EscapedPath() == "/x/passport-login/web/crossDomain"
}

func trustedBiliQRCallback(v string) bool {
	if trustedBiliCrossDomain(v) {
		return true
	}
	if !trustedBiliURL(v) {
		return false
	}
	u, _ := url.Parse(v)
	// Historical QR callbacks may use passport.bilibili.com or www.bilibili.com,
	// but arbitrary Bilibili subdomains are not trusted sources of credentials.
	host := strings.ToLower(u.Hostname())
	return host == "passport.bilibili.com" || host == "www.bilibili.com"
}

var cookieKeys = map[string]bool{"SESSDATA": true, "bili_jct": true, "DedeUserID": true, "DedeUserID__ckMd5": true, "sid": true, "buvid3": true, "buvid4": true}

// Only allow cookies returned by Bilibili's QR flow. Never send a URL,
// qrcode_key, ticket, or cookie back to the browser or application logs.
func mergeBiliCookies(dst map[string]string, resp *http.Response) {
	if resp == nil {
		return
	}
	for _, c := range resp.Cookies() {
		if cookieKeys[c.Name] && c.Value != "" && c.MaxAge >= 0 && len(c.Value) <= 4096 && !strings.ContainsAny(c.Value, "\r\n;") {
			dst[c.Name] = c.Value
		}
	}
}

func collectBiliQRValues(rawURL string, resp *http.Response) (map[string]string, error) {
	if !trustedBiliQRCallback(rawURL) {
		return nil, errors.New("B站扫码返回了不受信任的跳转地址")
	}
	parsed, _ := url.Parse(rawURL)
	values := map[string]string{}
	for k, arr := range parsed.Query() {
		if cookieKeys[k] && len(arr) > 0 && arr[0] != "" && len(arr[0]) <= 4096 && !strings.ContainsAny(arr[0], "\r\n;") {
			values[k] = arr[0]
		}
	}
	mergeBiliCookies(values, resp)
	return values, nil
}

func formatBiliQRCookie(values map[string]string) (string, error) {
	if values["SESSDATA"] == "" {
		return "", errors.New("B站已确认扫码，但未获取到 SESSDATA 登录会话；请重新生成二维码")
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

func cookieFromQR(rawURL string, resp *http.Response) (string, error) {
	values, err := collectBiliQRValues(rawURL, resp)
	if err != nil {
		return "", err
	}
	return formatBiliQRCookie(values)
}

// In Bilibili's newer QR flow the URL contains a one-time ticket instead of
// session cookies. Set-Cookie is returned by crossDomain (often on an HTTP
// 302), so following redirects automatically would discard those headers.
// Use a separate no-redirect client and never request a non-allowlisted URL.
func (a *App) completeBiliQRCookie(ctx context.Context, rawURL string, poll *http.Response) (string, error) {
	values, err := collectBiliQRValues(rawURL, poll)
	if err != nil {
		return "", err
	}
	if values["SESSDATA"] != "" {
		return formatBiliQRCookie(values)
	}
	if !trustedBiliCrossDomain(rawURL) {
		return "", errors.New("B站扫码成功响应没有登录 Cookie，也不是支持的官方跨域回调")
	}

	client := *a.biliClient()
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	endpoint := rawURL
	for n := 0; n < 3; n++ {
		if !trustedBiliCrossDomain(endpoint) {
			return "", errors.New("B站扫码跨域登录跳转地址不受信任")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", errors.New("B站扫码跨域登录请求无效")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/135.0.0.0 Safari/537.36")
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("Origin", "https://www.bilibili.com")
		response, err := client.Do(req)
		if err != nil {
			return "", errors.New("B站扫码跨域回调请求失败，请重新扫码")
		}
		mergeBiliCookies(values, response)
		location := response.Header.Get("Location")
		status := response.StatusCode
		_ = response.Body.Close()
		if values["SESSDATA"] != "" {
			return formatBiliQRCookie(values)
		}
		if status < 300 || status >= 400 || location == "" {
			break
		}
		base, _ := url.Parse(endpoint)
		next, err := url.Parse(location)
		if err != nil {
			break
		}
		endpoint = base.ResolveReference(next).String()
	}
	return formatBiliQRCookie(values)
}

func (a *App) qrRoutes(mux *http.ServeMux) {
	start := func(w http.ResponseWriter, r *http.Request) {
        if !a.isAdmin(r) { send(w,403,map[string]string{"error":"forbidden"});return }
        target:="bilibili"
        if r.Method==http.MethodPost {
          var body struct{ InstanceID string `json:"instance_id"` }
          if e:=decode(r,&body);e!=nil {send(w,400,map[string]string{"error":e.Error()});return}
          if body.InstanceID!=""{target=body.InstanceID}
        }
        a.mu.RLock()
        item,exists:=a.config.listener(target)
        a.mu.RUnlock()
        if !exists||item.Platform!="bilibili"{
          send(w,400,map[string]string{"error":"请先保存该 B站直播间实例后再扫码"});return
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
		a.qrSession = biliQRSession{TargetID:target,Key: resp.Data.QRCodeKey, Expiry: time.Now().Add(4 * time.Minute)}
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
        target:=a.qrSession.TargetID
        if target==""{target="bilibili"}
		if key == "" || time.Now().After(a.qrSession.Expiry) {
			a.qrSession = biliQRSession{}
			send(w, 410, map[string]string{"error": "扫码会话已过期，请重新生成"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
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
		cookie, e := a.completeBiliQRCookie(ctx, reply.Data.URL, resp)
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
        before:=a.config
        candidate:=a.config
        candidate.Listeners=append([]ListenerConfig(nil),before.Listeners...)
        e=candidate.assignBiliCookie(target,cookie)
        if e==nil{e=validateRoomListeners(&candidate)}
        if e!=nil{
          a.mu.Unlock()
          send(w,409,map[string]string{"error":"账号 Cookie 冲突或房间失效："+e.Error()})
          return
        }
        a.config=candidate
        e=a.saveLocked()
        if e!=nil{a.config=before}
        cfg:=a.config
        a.mu.Unlock()
        if e!=nil{send(w,500,map[string]string{"error":"本地保存登录状态失败"});return}
        a.qrSession=biliQRSession{}
        item,_:=cfg.listener(target)
        if item.Enabled {a.apply(cfg)}
		send(w, 200, map[string]any{"status": "success", "uid": nav.Data.Mid, "username": nav.Data.Uname, "message": "B站扫码登录成功，已安全保存会话"})
	})
	mux.HandleFunc("POST /api/bili/logout", func(w http.ResponseWriter,r *http.Request){
 if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
 var req struct{InstanceID string `json:"instance_id"`}
 if err:=decode(r,&req);err!=nil{send(w,400,map[string]string{"error":err.Error()});return}
 id:=req.InstanceID
 if id==""{id="bilibili"}
 a.qrMu.Lock()
 if a.qrSession.TargetID==id{a.qrSession=biliQRSession{}}
 a.qrMu.Unlock()
 a.mu.Lock()
 before:=a.config
 updated:=before
 updated.Listeners=append([]ListenerConfig(nil),before.Listeners...)
 if err:=updated.assignBiliCookie(id,"");err!=nil{
  a.mu.Unlock()
  send(w,404,map[string]string{"error":err.Error()});return
 }
 // An enabled room without a credential cannot remain active when multiple
 // Bilibili rooms are configured; logging out disables only that room.
 if id=="bilibili"{updated.Bilibili.Enabled=false}else{
  for i:=range updated.Listeners{
   if updated.Listeners[i].ID==id{updated.Listeners[i].Enabled=false}
  }
 }
 a.config=updated
 err:=a.saveLocked()
 if err!=nil{a.config=before}
 a.mu.Unlock()
 if err!=nil{send(w,500,map[string]string{"error":"保存失败"});return}
 a.apply(updated)
 send(w,200,map[string]string{"status":"ok"})
})

}

// Legacy call-site compatibility; reconfigure connections from the complete
// multi-room state, rather than replacing a platform-wide singleton worker.
func (a *App) restartBilibili(_ PlatformConfig) {
 a.mu.RLock()
 cfg:=a.config
 a.mu.RUnlock()
 a.apply(cfg)
}
