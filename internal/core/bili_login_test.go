package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBiliQRLoginLifecycle(t *testing.T) {
	var pollCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.bilibili.com/qrcode/secure","qrcode_key":"server-private-key"}}`))
		case "/poll":
			if r.URL.Query().Get("qrcode_key") != "server-private-key" {
				t.Error("wrong poll key")
			}
			if pollCount.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"code":0,"data":{"code":86101,"message":"not scanned"}}`))
				return
			}
			w.Header().Add("Set-Cookie", "SESSDATA=trusted-from-server; Path=/; HttpOnly")
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"url":"https://www.bilibili.com/?SESSDATA=url-session&DedeUserID=18&bili_jct=abc"}}`))
		case "/nav":
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=trusted-from-server") {
				t.Error("nav is not sent authenticated cookie")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"isLogin":true,"mid":18,"uname":"bili-test"}}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	app := New(dir, "0.6.0", "owner/repo")
	app.qrGenerateURL = upstream.URL + "/generate"
	app.qrPollURL = upstream.URL + "/poll"
	app.qrNavURL = upstream.URL + "/nav"
	server := httptest.NewServer(app.Routes(http.NotFoundHandler()))
	defer server.Close()
	post := func(path string) (int, string) {
		t.Helper()
		r, e := http.Post(server.URL+path, "application/json", strings.NewReader("{}"))
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var data map[string]any
		if e = json.NewDecoder(r.Body).Decode(&data); e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(data)
		return r.StatusCode, string(b)
	}
	code, p := post("/api/bili/qr/start")
	if code != 200 || !strings.Contains(p, "passport.bilibili.com/qrcode") || strings.Contains(p, "server-private-key") {
		t.Fatalf("QR response leaked key or invalid: %d %s", code, p)
	}
	code, p = post("/api/bili/qr/poll")
	if code != 200 || !strings.Contains(p, "waiting") {
		t.Fatalf("pending: %d %s", code, p)
	}
	code, p = post("/api/bili/qr/poll")
	if code != 200 || !strings.Contains(p, "success") || strings.Contains(p, "SESSDATA") {
		t.Fatalf("success: %d %s", code, p)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "SESSDATA=trusted-from-server") {
		t.Fatal("no persisted cookie")
	}
	r, err := http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var cfg map[string]any
	if err = json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg)
	if strings.Contains(string(b), "SESSDATA") {
		t.Fatal("secret exposed by config GET")
	}
	if code, _ := post("/api/bili/logout"); code != 200 {
		t.Fatalf("logout status %d", code)
	}
	if app.config.Bilibili.Cookie != "" {
		t.Fatal("logout did not clear cookie")
	}
	if code, _ := post("/api/bili/qr/poll"); code != 410 {
		t.Fatalf("poll with expired state returns %d", code)
	}
}
func TestBiliQRRejectUnverifiedOrUntrusted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://www.bilibili.com/scan","qrcode_key":"abc"}}`))
		case "/poll":
			w.Header().Set("Set-Cookie", "SESSDATA=unverified")
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"url":"https://www.bilibili.com/?SESSDATA=unverified"}}`))
		case "/nav":
			_, _ = w.Write([]byte(`{"code":0,"data":{"isLogin":false,"mid":0}}`))
		}
	}))
	defer upstream.Close()
	app := New(t.TempDir(), "0.6.0", "repo")
	app.config.Bilibili.Cookie = "previous"
	app.qrGenerateURL = upstream.URL + "/generate"
	app.qrPollURL = upstream.URL + "/poll"
	app.qrNavURL = upstream.URL + "/nav"
	server := httptest.NewServer(app.Routes(http.NotFoundHandler()))
	defer server.Close()
	do := func(p string, origin string) int {
		r, _ := http.NewRequest("POST", server.URL+p, strings.NewReader("{}"))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if status := do("/api/bili/qr/start", "https://evil.example"); status != 403 {
		t.Fatalf("CSRF accepted: %d", status)
	}
	if status := do("/api/bili/qr/start", ""); status != 200 {
		t.Fatal(status)
	}
	if status := do("/api/bili/qr/poll", ""); status != 502 {
		t.Fatal(status)
	}
	if app.config.Bilibili.Cookie != "previous" {
		t.Fatal("failed verification modified saved session")
	}
	if trustedBiliURL("https://evil.bilibili.com.attacker.net/scan") || trustedBiliURL("http://www.bilibili.com/scan") || trustedBiliURL("https://user@bilibili.com/scan") {
		t.Fatal("untrusted login URI accepted")
	}
	if _, err := cookieFromQR("https://attacker.example/?SESSDATA=steal", &http.Response{}); err == nil {
		t.Fatal("untrusted poll redirect accepted")
	}
	app.qrMu.Lock()
	app.qrSession.Expiry = time.Now().Add(-time.Second)
	app.qrMu.Unlock()
	if status := do("/api/bili/qr/poll", ""); status != 410 {
		t.Fatalf("expected expired: %d", status)
	}
}
