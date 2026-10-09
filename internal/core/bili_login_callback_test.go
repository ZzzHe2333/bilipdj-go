package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type callbackTransport func(*http.Request) (*http.Response, error)

func (f callbackTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQRCallbacksAllowOnlyOfficialCrossDomain(t *testing.T) {
	valid := []string{
		"https://passport.biligame.com/crossDomain?SESSDATA=legacy&bili_jct=csrf",
		"https://passport.biligame.com/x/passport-login/web/crossDomain?ticket=one-time",
		"https://passport.bilibili.com/x/passport-login/web/crossDomain?ticket=x",
	}
	for _, endpoint := range valid {
		if !trustedBiliCrossDomain(endpoint) {
			t.Errorf("official callback rejected: %s", endpoint)
		}
	}
	invalid := []string{
		"http://passport.biligame.com/crossDomain?ticket=x",
		"https://passport.biligame.com.attacker.example/crossDomain?ticket=x",
		"https://user@passport.biligame.com/crossDomain?ticket=x",
		"https://passport.biligame.com:8443/crossDomain?ticket=x",
		"https://passport.biligame.com/%63rossDomain?ticket=x",
		"https://evil.example/x/passport-login/web/crossDomain?ticket=x",
		"https://www.bilibili.com/x/passport-login/web/crossDomain?ticket=x",
		"https://passport.biligame.com/crossDomain#frag",
	}
	for _, endpoint := range invalid {
		if trustedBiliCrossDomain(endpoint) {
			t.Errorf("unexpected trusted cross-domain callback: %s", endpoint)
		}
	}
	if !trustedBiliQRCallback(valid[0]) {
		t.Fatal("official biligame callback rejected")
	}
	if got, e := cookieFromQR(valid[0], &http.Response{}); e != nil || !strings.Contains(got, "SESSDATA=legacy") {
		t.Fatalf("legacy query credentials failed: %q %v", got, e)
	}
}

func TestQRNewTicket302CookiesAndPersist(t *testing.T) {
	var callbackHits atomic.Int32
	var navHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			_, _ = io.WriteString(w, `{"code":0,"data":{"url":"https://passport.bilibili.com/h5-app/passport/login/scan?navhide=1","qrcode_key":"qr-test-secret"}}`)
		case "/poll":
			_, _ = io.WriteString(w, `{"code":0,"data":{"code":0,"url":"https://passport.biligame.com/x/passport-login/web/crossDomain?ticket=secret-ticket&gourl=https%3A%2F%2Fwww.bilibili.com"}}`)
		case "/nav":
			navHits.Add(1)
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=token-from-302") || !strings.Contains(r.Header.Get("Cookie"), "DedeUserID=123") {
				t.Errorf("missing verified cookies in nav (do not log real credentials)")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"isLogin":true,"mid":123,"uname":"tested"}}`)
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	app := New(t.TempDir(), "0.6.1", "test/repo")
	app.qrGenerateURL, app.qrPollURL, app.qrNavURL = upstream.URL+"/generate", upstream.URL+"/poll", upstream.URL+"/nav"
	app.qrClient = &http.Client{Timeout: 6 * time.Second, Transport: callbackTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "passport.biligame.com" {
			callbackHits.Add(1)
			if req.URL.Path != "/x/passport-login/web/crossDomain" || req.URL.Query().Get("ticket") != "secret-ticket" || !strings.HasPrefix(req.Header.Get("Referer"), "https://www.bilibili.com") {
				t.Errorf("bad ticket URL or headers (ticket never displayed)")
			}
			h := http.Header{}
			h.Add("Set-Cookie", "SESSDATA=token-from-302; HttpOnly; Secure; Path=/")
			h.Add("Set-Cookie", "bili_jct=csrf-from-302; Path=/")
			h.Add("Set-Cookie", "DedeUserID=123; Path=/")
			h.Set("Location", "https://evil.example/steal")
			return &http.Response{StatusCode: http.StatusFound, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		if req.URL.Hostname() == "evil.example" {
			t.Error("followed untrusted redirect")
			return nil, io.EOF
		}
		return http.DefaultTransport.RoundTrip(req)
	})}
	handler := httptest.NewServer(app.Routes(http.NotFoundHandler()))
	defer handler.Close()
	for _, endpoint := range []string{"/api/bili/qr/start", "/api/bili/qr/poll"} {
		resp, e := http.Post(handler.URL+endpoint, "application/json", strings.NewReader("{}"))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s status %d: %s", endpoint, resp.StatusCode, string(b))
		}
		if strings.Contains(string(b), "token-from-302") || strings.Contains(string(b), "secret-ticket") {
			t.Fatal("secret exposed through response")
		}
		if endpoint == "/api/bili/qr/poll" {
			var msg map[string]any
			_ = json.Unmarshal(b, &msg)
			if msg["status"] != "success" || msg["uid"] != float64(123) {
				t.Fatalf("bad success result: %s", string(b))
			}
		}
	}
	if callbackHits.Load() != 1 || navHits.Load() != 1 {
		t.Fatalf("callback count=%d nav=%d", callbackHits.Load(), navHits.Load())
	}
	data, e := os.ReadFile(filepath.Join(app.dataPath))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), "SESSDATA=token-from-302") {
		t.Fatal("successful session not persisted")
	}
}

func TestQRUntrustedRedirectCannotCauseOutboundRequestOrOverwrite(t *testing.T) {
	var count atomic.Int32
	app := New(t.TempDir(), "0.6.1", "test/repo")
	app.qrClient = &http.Client{Transport: callbackTransport(func(req *http.Request) (*http.Response, error) {
		count.Add(1)
		h := http.Header{}
		h.Set("Location", "https://attacker.example/steal")
		return &http.Response{StatusCode: 302, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	_, err := app.completeBiliQRCookie(context.Background(), "https://passport.biligame.com/x/passport-login/web/crossDomain?ticket=t", nil)
	if err == nil {
		t.Fatal("must reject callback with no cookies")
	}
	if count.Load() != 1 {
		t.Fatalf("unsafe follow: %d calls", count.Load())
	}
	_, err = app.completeBiliQRCookie(context.Background(), "https://passport.biligame.com.attacker.example/x/passport-login/web/crossDomain?ticket=t", nil)
	if err == nil || count.Load() != 1 {
		t.Fatal("untrusted URL made a network request")
	}
}
