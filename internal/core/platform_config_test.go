package core

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestMultiplatformVisibleConfigsPersistAndUnimplementedStayDisabled(t *testing.T) {
    a := New(t.TempDir(), "0.10.1", "org/demo")
    c := defaultConfig()
    c.Bilibili.Room = ""
    c.Douyin.Room = "https://live.douyin.com/123456?from=share"
    c.Huya.Room = "123"
    c.VisiblePlatforms = []string{"huya", "bilibili", "huya", "douyin", "wechat_mp", "kuaishou", "douyu"}
    cleaned, err := cleanConfig(c)
    if err != nil { t.Fatal(err) }
    if len(cleaned.VisiblePlatforms) != 6 { t.Fatalf("platform entries not deduplicated: %v", cleaned.VisiblePlatforms) }
    c = cleaned
    body, _ := json.Marshal(c)
    req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9816/api/config", bytes.NewReader(body))
    req.RemoteAddr = "127.0.0.1:22222"
    w := httptest.NewRecorder()
    a.Routes(http.NotFoundHandler()).ServeHTTP(w, req)
    if w.Code != 200 { t.Fatalf("save status %d: %s", w.Code, w.Body.String()) }
    b := New(t.TempDir(), "0.10.1", "org/demo")
    _ = b
    a.mu.RLock()
    saved := a.config
    a.mu.RUnlock()
    if len(saved.VisiblePlatforms) != 6 || saved.Huya.Room != "123" { t.Fatalf("visible platforms not persisted to state: %+v", saved) }
    c.Huya.Enabled = true
    if _, err := cleanConfig(c); err == nil { t.Fatal("unimplemented Huya listener must not be enabled") }
    c.Huya.Enabled = false
    c.VisiblePlatforms = append(c.VisiblePlatforms, "invalid")
    if _, err := cleanConfig(c); err == nil { t.Fatal("unknown platform must be rejected") }
}

func TestPlatformGetConfigMasksCredentials(t *testing.T) {
    a := New(t.TempDir(), "0.10.1", "org/demo")
    a.config.Bilibili.Cookie = "SESSDATA=secret"
    a.config.Huya.Cookie = "secret"
    req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9816/api/config", nil)
    w := httptest.NewRecorder()
    a.Routes(http.NotFoundHandler()).ServeHTTP(w, req)
    if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("secret")) {
        t.Fatalf("config GET leaked credentials: %d %s", w.Code, w.Body.String())
    }
}
