package core

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestManualSourceEditKeepsOriginAndSurvivesRestart(t *testing.T) {
 dir := t.TempDir()
 a := New(dir,"0.10.1","org/demo")
 handler := a.Routes(http.NotFoundHandler())
 post := func(body string) (*httptest.ResponseRecorder, []QueueItem) {
  t.Helper()
  req := httptest.NewRequest(http.MethodPost,"http://127.0.0.1:9816/api/queue",bytes.NewBufferString(body))
  req.RemoteAddr = "127.0.0.1:4567"
  w := httptest.NewRecorder()
  handler.ServeHTTP(w,req)
  var items []QueueItem
  if w.Code==200 { if err:=json.Unmarshal(w.Body.Bytes(),&items);err!=nil {t.Fatal(err)} }
  return w,items
 }
 if w,items:=post(`{"action":"add","name":"原用户名"}`);w.Code!=200||len(items)!=1||items[0].Platform!="manual"||items[0].SourcePlatform!="" {t.Fatalf("add: %d %+v",w.Code,items)}
 key:=a.queue[0].Key
 raw,_:=json.Marshal(map[string]any{"action":"edit","key":key,"new_name":"新用户名","note":"排队备注","source_platform":"bilibili"})
 w,items:=post(string(raw))
 if w.Code!=200||items[0].Username!="新用户名"||items[0].SourcePlatform!="bilibili"||items[0].Platform!="manual"||items[0].UserID!=""||items[0].Key!=key {t.Fatalf("edited identity: %d %+v",w.Code,items)}
 b:=New(dir,"0.10.1","org/demo")
 if len(b.queue)!=1||b.queue[0].SourcePlatform!="bilibili"||b.queue[0].Key!=key {t.Fatalf("persisted: %+v",b.queue)}
 raw,_=json.Marshal(map[string]any{"action":"edit","key":key,"note":"only note"})
 w,items=post(string(raw))
 if w.Code!=200||items[0].SourcePlatform!="bilibili"||items[0].Username!="新用户名" {t.Fatalf("legacy client cleared data: %d %+v",w.Code,items)}
 raw,_=json.Marshal(map[string]any{"action":"edit","key":key,"note":"","source_platform":""})
 w,items=post(string(raw))
 if w.Code!=200||items[0].SourcePlatform!="" {t.Fatalf("clear source: %d %+v",w.Code,items)}
}
func TestQueueEditCannotSpoofAuthenticatedLiveIdentity(t *testing.T) {
 a:=New(t.TempDir(),"0.10.1","org/demo")
 a.queue=[]QueueItem{{Key:"bilibili:123",Platform:"bilibili",UserID:"123",Username:"主播"}}
 h:=a.Routes(http.NotFoundHandler())
 post:=func(v any) *httptest.ResponseRecorder {
  t.Helper(); data,_:=json.Marshal(v)
  req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:9816/api/queue",bytes.NewReader(data));req.RemoteAddr="127.0.0.1:4567"
  w:=httptest.NewRecorder();h.ServeHTTP(w,req);return w
 }
 for _,p:=range []map[string]any{
  {"action":"edit","key":"bilibili:123","note":"x","new_name":"伪造用户名"},
  {"action":"edit","key":"bilibili:123","note":"x","source_platform":"douyin"},
 } {
  w:=post(p);if w.Code!=400 {t.Fatalf("expected live identity reject, got %d",w.Code)}
 }
 if a.queue[0].Username!="主播"||a.queue[0].Platform!="bilibili"||a.queue[0].Note!="" {t.Fatalf("unauthorized change: %+v",a.queue[0])}
 if w:=post(map[string]any{"action":"edit","key":"bilibili:123","note":"允许备注"});w.Code!=200||a.queue[0].Note!="允许备注" {t.Fatalf("note edit incompatible: %d %+v",w.Code,a.queue[0])}
}
func TestQueueEditRejectsInvalidManualSourceAndNameWithoutMutation(t *testing.T) {
 a:=New(t.TempDir(),"0.10.1","org/demo")
 a.queue=[]QueueItem{{Key:"manual:123",Platform:"manual",Username:"成员",Note:"原备注"}}
 h:=a.Routes(http.NotFoundHandler())
 for _,update:=range []map[string]any{
  {"action":"edit","key":"manual:123","note":"新备注","new_name":"意外修改","source_platform":"untrusted"},
  {"action":"edit","key":"manual:123","note":"新备注","new_name":" "},
 } {
  raw,_:=json.Marshal(update)
  req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:9816/api/queue",bytes.NewReader(raw));req.RemoteAddr="127.0.0.1:1234"
  w:=httptest.NewRecorder();h.ServeHTTP(w,req)
  if w.Code!=400||a.queue[0].Username!="成员"||a.queue[0].Note!="原备注"||a.queue[0].SourcePlatform!="" {t.Fatalf("partial edit accepted: %d %+v",w.Code,a.queue[0])}
 }
}
