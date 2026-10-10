package core

import (
    "bytes"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ZzzHe2333/bilipdj-go/internal/autostart"
)

type mockAutostart struct {
    supported bool
    enabled bool
    setCalls int
    fail error
}
func (m *mockAutostart) Status() (autostart.State,error) {
    return autostart.State{Supported:m.supported,Enabled:m.enabled,Platform:"test"},nil
}
func (m *mockAutostart) Set(on bool) error {
    m.setCalls++
    if m.fail!=nil { return m.fail }
    m.enabled=on
    return nil
}

func startupRequest(h http.Handler, method, body, addr string) *httptest.ResponseRecorder {
    req:=httptest.NewRequest(method,"http://127.0.0.1:9816/api/autostart",bytes.NewBufferString(body))
    req.RemoteAddr=addr
    req.Header.Set("Origin","http://127.0.0.1:9816")
    w:=httptest.NewRecorder()
    h.ServeHTTP(w,req)
    return w
}
func TestAutostartAuthDefaultAndToggle(t *testing.T) {
    a:=New(t.TempDir(),"test","org/demo")
    fake:=&mockAutostart{supported:true}
    a.SetAutostartController(fake)
    h:=a.Routes(http.NotFoundHandler())
    // Reading or just launching the app must never enable startup.
    before:=startupRequest(h,"GET","","127.0.0.1:12000")
    if before.Code!=200||!bytes.Contains(before.Body.Bytes(),[]byte(`"enabled":false`))||fake.setCalls!=0 {
        t.Fatalf("default must be disabled, no writes: %d %s",before.Code,before.Body.String())
    }
    if before.Header().Get("Cache-Control")!="no-store" { t.Fatal("autostart state should not be cached") }
    bad:=startupRequest(h,"POST",`{"enabled":true}`,"192.0.2.19:12345")
    if bad.Code!=403||fake.setCalls!=0 { t.Fatalf("remote without auth changed state: %d",bad.Code) }
    for _,body:=range []string{`{}`,`{"enabled":"true"}`,`{"enabled":null}`} {
        w:=startupRequest(h,"POST",body,"127.0.0.1:2222")
        if w.Code!=400||fake.setCalls!=0 { t.Fatalf("bad payload %q: %d",body,w.Code) }
    }
    enable:=startupRequest(h,"POST",`{"enabled":true}`,"127.0.0.1:2222")
    if enable.Code!=200||!fake.enabled||!bytes.Contains(enable.Body.Bytes(),[]byte(`"enabled":true`)){t.Fatalf("cannot enable: %d %s",enable.Code,enable.Body.String())}
    disable:=startupRequest(h,"POST",`{"enabled":false}`,"127.0.0.1:2222")
    if disable.Code!=200||fake.enabled||fake.setCalls!=2{t.Fatalf("cannot disable: %d %s",disable.Code,disable.Body.String())}
}
func TestAutostartUnsupportedAndError(t *testing.T) {
    a:=New(t.TempDir(),"test","org/demo")
    fake:=&mockAutostart{supported:false}
    a.SetAutostartController(fake)
    h:=a.Routes(http.NotFoundHandler())
    w:=startupRequest(h,"POST",`{"enabled":true}`,"127.0.0.1:2222")
    if w.Code!=409||fake.setCalls!=0 {t.Fatalf("unsupported %d",w.Code)}
    fake.supported=true
    fake.fail=errors.New("read-only registry")
    w=startupRequest(h,"POST",`{"enabled":true}`,"127.0.0.1:2222")
    if w.Code!=500||fake.enabled {t.Fatalf("write failure %d",w.Code)}
}
