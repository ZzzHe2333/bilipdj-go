package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func onboardingRequest(t *testing.T, handler http.Handler, method, payload, remote string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://127.0.0.1:9816/api/onboarding", bytes.NewBufferString(payload))
	request.RemoteAddr = remote
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
func TestOnboardingFirstRunSkipAndPersistence(t *testing.T) {
	dir := t.TempDir()
	app := New(dir, "test", "demo")
	handler := app.Routes(http.NotFoundHandler())
	if app.onboardingCompleted {
		t.Fatal("new installation must show wizard")
	}
	if res := onboardingRequest(t, handler, http.MethodPost, `{"completed":true}`, "192.0.2.1:9000"); res.Code != 403 {
		t.Fatalf("remote user must not finish wizard: %d", res.Code)
	}
	if res := onboardingRequest(t, handler, http.MethodPost, `{"completed":false}`, "127.0.0.1:9000"); res.Code != 400 {
		t.Fatalf("cannot reset wizard via endpoint: %d", res.Code)
	}
	if res := onboardingRequest(t, handler, http.MethodPost, `{"completed":true}`, "127.0.0.1:9000"); res.Code != 200 {
		t.Fatalf("completion failed: %d: %s", res.Code, res.Body)
	}
	if !New(dir, "test", "demo").onboardingCompleted {
		t.Fatal("wizard completion lost on restart")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data["onboarding_completed"] != true {
		t.Fatalf("not saved: %v %v", data, err)
	}
}
func TestOnboardingExistingLegacyStateDoesNotInterrupt(t *testing.T) {
	dir := t.TempDir()
	old := New(dir, "test", "demo")
	if err := old.saveLocked(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(original, &p); err != nil {
		t.Fatal(err)
	}
	delete(p, "onboarding_completed")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	migrated := New(dir, "test", "demo")
	if !migrated.onboardingCompleted {
		t.Fatal("old installation should not be interrupted on upgrade")
	}
}
