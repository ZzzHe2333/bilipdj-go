package core

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/ZzzHe2333/bilipdj-go/internal/perf"
)

func TestPerformanceEndpointRequiresAdmin(t *testing.T) {
 a:=New(t.TempDir(),"0.10.3","org/demo")
 h:=a.Routes(http.NotFoundHandler())
 get:=func(remote,token string)*httptest.ResponseRecorder{
  t.Helper()
  req:=httptest.NewRequest(http.MethodGet,"http://127.0.0.1:9816/api/performance",nil)
  req.RemoteAddr=remote
  if token!=""{req.Header.Set("X-Admin-Token",token)}
  w:=httptest.NewRecorder()
  h.ServeHTTP(w,req)
  return w
 }
 t.Setenv("BILIPDJ_ADMIN_TOKEN","test-secret")
 if w:=get("192.168.1.13:33333","");w.Code!=403{t.Fatalf("unauthorized remote read: %d",w.Code)}
 if w:=get("192.168.1.13:33333","invalid");w.Code!=403{t.Fatalf("incorrect token accepted: %d",w.Code)}
 w:=get("192.168.1.13:33333","test-secret")
 if w.Code!=200{t.Fatalf("authorized read: %d %s",w.Code,w.Body.String())}
 if w.Header().Get("Cache-Control")!="no-store"{t.Fatal("metrics must not be cached")}
 var result perf.Snapshot
 if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
 if result.At.IsZero()||result.NPU.Available {t.Fatalf("unexpected payload: %+v",result)}
 if local:=get("127.0.0.1:12345","");local.Code!=200{t.Fatalf("loopback should remain allowed: %d",local.Code)}
}
