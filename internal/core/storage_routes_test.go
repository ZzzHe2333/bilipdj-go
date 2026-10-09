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
func TestStorageChoiceRequiresAdminAndNeverUsesPythonRoot(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "portable")
	old := filepath.Join(appDir, "data")
	env := map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "xdg")}
	newDir := filepath.Join(env["XDG_DATA_HOME"], "bilipdj-go")
	oldShared := filepath.Join(env["XDG_DATA_HOME"], "bilipdj")
	for path, content := range map[string]string{
		filepath.Join(oldShared, "state.json"): `{"config":{"command":"legacy"}}`,
		filepath.Join(newDir, "state.json"):    `{"config":{"command":"current"}}`,
	} {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p, e := storage.PlanFor(appDir, old, "", "linux", dir, env)
	if e != nil || !p.Conflict || p.Active != newDir {
		t.Fatal(p, e)
	}
	a := New(newDir, "test", "repo")
	if err := a.SetStoragePlan(p); err != nil {
		t.Fatal(err)
	}
	h := a.Routes(http.NotFoundHandler())
	sendChoice := func(choice, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://localhost/api/storage/choice", strings.NewReader(`{"choice":"`+choice+`"}`))
		req.RemoteAddr = remote
		req.Host = "localhost"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if got := sendChoice("user", "10.1.2.3:1234"); got.Code != 403 {
		t.Fatalf("remote: %d", got.Code)
	}
	if got := sendChoice("legacy", "127.0.0.1:4242"); got.Code != 400 {
		t.Fatalf("must reject legacy: %d %s", got.Code, got.Body)
	}
	if got := sendChoice("user", "127.0.0.1:4242"); got.Code != 200 {
		t.Fatalf("user: %d %s", got.Code, got.Body)
	}
	after, e := storage.PlanFor(appDir, old, "", "linux", dir, env)
	if e != nil || after.Conflict || after.Active != newDir {
		t.Fatal(after, e)
	}
	before, _ := os.ReadFile(filepath.Join(oldShared, "state.json"))
	if !strings.Contains(string(before), "legacy") {
		t.Fatal("old Python directory changed")
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

func TestPR314PythonCSVUsesLocalArchiveDirectory(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "local", "bilipdj", "archives")
	if err := os.MkdirAll(local, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(local, "queue_archive_slot_1.csv")
	original := []byte("\xef\xbb\xbf序号,id,内容,最后操作时间,来源平台\n1,LocalUser,,2026-10-09T15:00:00,douyin\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	goRoot := filepath.Join(root, "roaming", "bilipdj-go")
	goLocal := filepath.Join(root, "local", "bilipdj-go", "archives")
	a := New(goRoot, "0.10.1", "repo")
	p := storage.Plan{Mode: "managed", Active: goRoot, User: goRoot, ArchiveDir: goLocal, PythonArchiveDir: local, BackupDir: filepath.Join(root, "local", "bilipdj-go", "backups")}
	if err := a.SetStoragePlan(p); err != nil {
		t.Fatal(err)
	}
	h := a.Routes(http.NotFoundHandler())
	req := httptest.NewRequest("GET", "http://localhost/api/storage/python-queues", nil)
	req.RemoteAddr = "127.0.0.1:4343"
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "LocalUser") && !strings.Contains(rr.Body.String(), `"1":1`) {
		t.Fatalf("read local slot %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest("POST", "http://localhost/api/storage/python-queues/import", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:4343"
	req.Host = "localhost"
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("import local slot %d %s", rr.Code, rr.Body.String())
	}
	if len(a.queue) != 1 || a.queue[0].Platform != "douyin" {
		t.Fatalf("lost platform %+v", a.queue)
	}
	next, err := os.ReadFile(path)
	if err != nil || string(next) != string(original) {
		t.Fatal("Python archive mutated")
	}
	if _, err = os.Stat(filepath.Join(goLocal, "go-queue-state.json")); err != nil {
		t.Fatal("Go queue did not persist in Local", err)
	}
}
