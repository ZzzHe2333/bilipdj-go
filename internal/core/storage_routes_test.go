package core

import (
	"encoding/json"
	"github.com/ZzzHe2333/bilipdj-go/internal/storage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonQueueCSVPlatformRoundTrip(t *testing.T) {
	source := "\ufeff最后操作时间,2026-10-09T00:00:00\n操作人,test\n操作说明,join\n\n序号,id,内容,最后操作时间,来源平台\n1,同名用户,官服,2026-10-09T02:03:04,bilibili\n2,同名用户,B服,2026-10-09T02:05:06,douyin\n"
	items, e := queueCSV([]byte(source))
	if e != nil || len(items) != 2 {
		t.Fatalf("parsed=%v err=%v", items, e)
	}
	if items[0].Platform != "bilibili" || items[1].Platform != "douyin" || items[0].At.Year() != 2026 {
		t.Fatal(items)
	}
	out, e := encodePythonQueueCSV(items)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(out), "来源平台") || !strings.Contains(string(out), "douyin") {
		t.Fatal(string(out))
	}
	parsed, e := queueCSV(out)
	if e != nil || len(parsed) != 2 || parsed[1].Platform != "douyin" {
		t.Fatalf("roundtrip %v %v", parsed, e)
	}
}
func TestPythonImportDoesNotMutateSource(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "core", "cd", "queue_archive_slot_1.csv")
	if e := os.MkdirAll(filepath.Dir(csvPath), 0700); e != nil {
		t.Fatal(e)
	}
	content := []byte("\ufeff序号,id,内容,最后操作时间,来源平台\n1,Alice,官服,2026-10-09T02:03:04,douyin\n")
	if e := os.WriteFile(csvPath, content, 0600); e != nil {
		t.Fatal(e)
	}
	a := New(dir, "test", "repo")
	handler := a.Routes(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, "http://localhost/api/storage/python-queues", nil)
	req.RemoteAddr = "127.0.0.1:42000"
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("GET %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "http://localhost/api/storage/python-queues/import", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:42000"
	req.Host = "localhost"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("POST %d %s", rr.Code, rr.Body.String())
	}
	original, _ := os.ReadFile(csvPath)
	if string(original) != string(content) {
		t.Fatal("Python archive mutated")
	}
	raw, e := os.ReadFile(filepath.Join(dir, "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	var state persisted
	if e = json.Unmarshal(raw, &state); e != nil {
		t.Fatal(e)
	}
	if len(state.Queue) != 1 || state.Queue[0].Platform != "douyin" {
		t.Fatal(state.Queue)
	}
}
func TestStorageChoiceRequiresAdminAndRestart(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "portable")
	old := filepath.Join(appDir, "data")
	newDir := filepath.Join(dir, "xdg", "bilipdj")
	for _, target := range []string{filepath.Join(old, "state.json"), filepath.Join(newDir, "state.json")} {
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(target, []byte("{}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p, e := storage.PlanFor(appDir, old, "", "linux", dir, map[string]string{"XDG_DATA_HOME": filepath.Dir(newDir)})
	if e != nil || !p.Conflict {
		t.Fatal(p, e)
	}
	a := New(old, "test", "repo")
	a.SetStoragePlan(p)
	h := a.Routes(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/storage/choice", strings.NewReader(`{"choice":"user"}`))
	req.RemoteAddr = "10.1.2.3:1234"
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("unauthorized HTTP %d", rr.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "http://localhost/api/storage/choice", strings.NewReader(`{"choice":"user"}`))
	req.RemoteAddr = "127.0.0.1:4242"
	req.Host = "localhost"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "restart_required") {
		t.Fatalf("choose HTTP %d %s", rr.Code, rr.Body.String())
	}
	after, e := storage.PlanFor(appDir, old, "", "linux", dir, map[string]string{"XDG_DATA_HOME": filepath.Dir(newDir)})
	if e != nil || after.Active != newDir {
		t.Fatal(after, e)
	}
}
func TestWebStylesSeparateFromTk(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, "test", "repo")
	a.SetStoragePlan(storage.Plan{Mode: "managed", Active: dir})
	if e := a.writeWebAppearanceFile("style", []byte(`{"text_color":"#123456"}`)); e != nil {
		t.Fatal(e)
	}
	if e := a.writeWebAppearanceFile("appearance", []byte(`{"mode":"light"}`)); e != nil {
		t.Fatal(e)
	}
	a2 := New(dir, "test", "repo")
	a2.SetStoragePlan(storage.Plan{Mode: "managed", Active: dir})
	if a2.style["text_color"] != "#123456" || a2.appearance["mode"] != "light" {
		t.Fatal(a2.style, a2.appearance)
	}
	if _, e := os.Stat(filepath.Join(dir, "style-win.json")); !os.IsNotExist(e) {
		t.Fatal("win style must not be written")
	}
}
