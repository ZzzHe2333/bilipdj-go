package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestDownloadSHA256(t *testing.T) {
	archive := []byte("zip contents test fixture")
	sum := sha256.Sum256(archive)
	name := fmt.Sprintf("bilipdj-go-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
	var addr string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			b, _ := json.Marshal(map[string]any{"tag_name": "v0.2.0", "assets": []map[string]any{{"name": name, "browser_download_url": addr + "/zip", "size": len(archive)}, {"name": name + ".sha256", "browser_download_url": addr + "/sha", "size": 100}}})
			w.Write(b)
		case "/zip":
			w.Write(archive)
		case "/sha":
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	addr = server.URL
	s := New("org/demo", "0.1.0", t.TempDir())
	s.APIBase = server.URL
	info, e := s.Check(context.Background())
	if e != nil || !info.UpdateAvailable {
		t.Fatalf("check: %v %+v", e, info)
	}
	res, e := s.Download(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(res.File, "0.2.0") {
		t.Fatal(res)
	}
	archive = []byte("tampered archive")
	if _, e = s.Download(context.Background()); e == nil || !strings.Contains(e.Error(), "SHA256") {
		t.Fatalf("tamper not rejected: %v", e)
	}
	_ = bytes.Compare
}
