package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyConfig = `server:
  host: "127.0.0.1"
  port: 9816
platform: bilibili
bilibili:
  roomid: 3049445
  uid: 0
  cookie: "SESSDATA=secret"
douyin:
  enabled: true
  live_id: "777123"
  cookie: 'ttwid=sensitive'
myjs:
  paidui_list_length_max: 35
  all_suoyourenbukepaidui: false
  admins:
    - "alpha"
    - "beta"
ui:
  language: 中文
queue_archive:
  active_slot: 2
`

func legacyZip(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	z := zip.NewWriter(buf)
	for name, raw := range map[string]string{
		"core/config.yaml":      legacyConfig,
		"core/cd/2.csv":         "#timestamp,2026-10-08\nseq,id,content,last_operation_at\n1,Alice,note,2026-10-08\n2,Bob,,2026-10-08\n",
		"core/cd/blacklist.csv": "seq,id,content,last_operation_at\n1,Eve,,2026-10-08\n",
		"appearance.json":       `{"schema":1,"mode":"light","design":"aurora"}`,
		"style.json":            `{"text_color":"#abcdef","queue_font_size":45}`,
		"core/kaiguan.yaml":     "enabled: true\n",
	} {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return buf.Bytes()
}
func TestLegacyParsingAndMapping(t *testing.T) {
	b := legacyZip(t)
	m, e := parseLegacy("old.zip", b, defaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	if m.cfg.Bilibili.Room != "3049445" || !m.cfg.Bilibili.Enabled || !m.cfg.Douyin.Enabled || m.cfg.Douyin.Room != "777123" {
		t.Fatalf("platforms: %+v", m.cfg)
	}
	if m.cfg.MaxQueue != 35 || m.cfg.ArchiveSlot != 2 || len(m.cfg.Admins) != 2 || len(m.cfg.Blacklist) != 1 {
		t.Fatalf("rules: %+v", m.cfg)
	}
	if len(m.queue) != 2 || m.queue[0].Username != "Alice" {
		t.Fatalf("queue: %+v", m.queue)
	}
	if m.style["text_color"] != "#abcdef" || m.appearance["mode"] != "light" {
		t.Fatalf("styles: %+v %+v", m.style, m.appearance)
	}
	if len(m.preview.Warnings) == 0 {
		t.Fatal("missing unsupported-feature warning")
	}
}
func TestLegacyImportEndpointAndBackup(t *testing.T) {
	dir := t.TempDir()
	app := New(dir, "0.2.0", "org/test")
	srv := app.Routes(http.NotFoundHandler())
	raw := legacyZip(t)
	request := func(path string, valid bool) *httptest.ResponseRecorder {
		buf := new(bytes.Buffer)
		mp := multipart.NewWriter(buf)
		f, e := mp.CreateFormFile("file", "old.zip")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(raw); e != nil {
			t.Fatal(e)
		}
		mp.Close()
		r := httptest.NewRequest("POST", "http://127.0.0.1:9816"+path, buf)
		r.Header.Set("Content-Type", mp.FormDataContentType())
		r.RemoteAddr = "127.0.0.1:9910"
		if !valid {
			r.RemoteAddr = "192.168.1.42:9920"
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}
	if w := request("/api/legacy/preview", false); w.Code != 403 {
		t.Fatalf("unsafe request %d", w.Code)
	}
	if w := request("/api/legacy/preview", true); w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body)
	}
	if _, e := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(e) {
		t.Fatal("preview changed disk")
	}
	if w := request("/api/legacy/import", true); w.Code != 200 {
		t.Fatalf("import %d %s", w.Code, w.Body)
	}
	reloaded := New(dir, "0.2.0", "org/test")
	if len(reloaded.queue) != 2 || reloaded.config.MaxQueue != 35 || reloaded.config.Blacklist[0] != "Eve" {
		t.Fatalf("import persistence %+v %+v", reloaded.config, reloaded.queue)
	}
	entries, e := os.ReadDir(filepath.Join(dir, "migration-backup"))
	if e != nil || len(entries) != 2 {
		t.Fatalf("backup files %v %v", entries, e)
	}
	for _, entry := range entries {
		fi, e := entry.Info()
		if e != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("backup permissions %v %v", entry, fi)
		}
	}
	cfg := httptest.NewRecorder()
	srv.ServeHTTP(cfg, httptest.NewRequest("GET", "http://127.0.0.1:9816/api/config", nil))
	if strings.Contains(cfg.Body.String(), "secret") || strings.Contains(cfg.Body.String(), "sensitive") {
		t.Fatalf("secret leaked: %s", cfg.Body.String())
	}
	var response map[string]any
	if e := json.Unmarshal(cfg.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	_ = io.Discard
}
func TestLegacyRejectMaliciousZip(t *testing.T) {
	buf := new(bytes.Buffer)
	z := zip.NewWriter(buf)
	w, e := z.Create("../config.yaml")
	if e != nil {
		t.Fatal(e)
	}
	w.Write([]byte(legacyConfig))
	z.Close()
	if _, e := parseLegacy("bad.zip", buf.Bytes(), defaultConfig()); e == nil {
		t.Fatal("accepted path traversal")
	}
}
