package core

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/ZzzHe2333/bilipdj-go/internal/perf"
)

func TestPerformanceEndpointRequiresAdmin(t *testing.T) {
 a:=New(t.TempDir(),"0.10.4","org/demo")
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
 if result.At.IsZero(){t.Fatalf("timestamp missing: %+v",result)}
 var fields map[string]json.RawMessage
 if err:=json.Unmarshal(w.Body.Bytes(),&fields);err!=nil{t.Fatal(err)}
 if len(fields)!=8 {t.Fatalf("expected 7 metrics plus timestamp, got %v",fields)}
 for _,k:=range []string{"cpu","memory","data_disk","disk_read","disk_write","project_disk","archive_disk"}{
  if _,ok:=fields[k];!ok{t.Fatalf("missing performance metric %q",k)}
 }
 for _,k:=range []string{"system_cpu","network_receive","network_send","gpu","npu"}{
  if _,ok:=fields[k];ok{t.Fatalf("removed system metric still exposed: %q",k)}
 }
 if local:=get("127.0.0.1:12345","");local.Code!=200{t.Fatalf("loopback should remain allowed: %d",local.Code)}
}
