package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func TestAcceleratedDiscoveryWithoutGithubAPI(t *testing.T) {
	name := fmt.Sprintf("bilipdj-go-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
	contents := []byte("test package archive")
	sum := sha256.Sum256(contents)
	manifest := map[string]any{"version": "0.10.0", "tag_name": "v0.10.0", "packages": map[string]any{name: map[string]any{"name": name, "size": len(contents), "sha256": hex.EncodeToString(sum[:])}}}
	js, _ := json.Marshal(manifest)
	used := []string{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = append(used, r.URL.String())
		var body []byte
		status := 200
		switch {
		case strings.Contains(r.URL.String(), "/releases/latest/download/update-manifest.json"):
			body = js
		case strings.HasSuffix(r.URL.Path, ".zip"):
			body = contents
		default:
			status = 503
			body = []byte("blocked")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	service := New("ZzzHe2333/bilipdj-go", "0.9.0", t.TempDir())
	service.Client = client
	checked, e := service.Check(context.Background(), "auto")
	if e != nil || !checked.UpdateAvailable || checked.Source != "accelerated" {
		t.Fatalf("failed proxy-only discovery %+v %v", checked, e)
	}
	got, e := service.Download(context.Background(), "accelerated")
	if e != nil || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("proxy download failed %+v %v", got, e)
	}
	if len(used) < 3 {
		t.Fatalf("proxy not used: %+v", used)
	}
}
