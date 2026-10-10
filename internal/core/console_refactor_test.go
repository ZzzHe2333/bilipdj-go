package core

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func consoleRequest(t *testing.T,app *App,method,path string,body any)*httptest.ResponseRecorder {
 t.Helper()
 var payload []byte
 if body!=nil{payload,_=json.Marshal(body)}
 req:=httptest.NewRequest(method,"http://127.0.0.1:9816"+path,bytes.NewReader(payload))
 req.RemoteAddr="127.0.0.1:60606"
 res:=httptest.NewRecorder()
 app.Routes(http.NotFoundHandler()).ServeHTTP(res,req)
 return res
}
func TestQueueReorderRequiresFullIdentityAndFreshOrdering(t *testing.T) {
 dir:=t.TempDir()
 app:=New(dir,"0.10.4","org/demo")
 for i,key:=range []string{"manual:one","manual:two","manual:three"}{
  app.queue=append(app.queue,QueueItem{Key:key,Username:string(rune('A'+i)),Platform:"manual",At:time.Now()})
 }
 if err:=app.saveLocked();err!=nil{t.Fatal(err)}
 before:=[]string{"manual:one","manual:two","manual:three"}
 after:=[]string{"manual:three","manual:one","manual:two"}
 for _,tc:=range []struct{keys,before []string;status int}{
  {[]string{"manual:one","manual:one","manual:three"},before,400},
  {after,[]string{"manual:two","manual:one","manual:three"},409},
  {after,before,200},
  {before,before,409},
 } {
  w:=consoleRequest(t,app,"POST","/api/queue",map[string]any{"action":"reorder","keys":tc.keys,"before":tc.before})
  if w.Code!=tc.status{t.Fatalf("reorder %v returned %d: %s",tc.keys,w.Code,w.Body.String())}
 }
 if app.queue[0].Key!="manual:three"{t.Fatal("saved order not reflected")}
 reloaded:=New(dir,"0.10.4","org/demo")
 if len(reloaded.queue)!=3||reloaded.queue[0].Key!="manual:three"{t.Fatalf("persisted order was lost: %+v",reloaded.queue)}
}
func TestStyleTenSlotsCanSavePreviewAndApply(t *testing.T){
 dir:=t.TempDir()
 app:=New(dir,"0.10.4","org/demo")
 for _,tc:=range []struct{slot string;size int}{{"1",43},{"10",52}} {
  w:=consoleRequest(t,app,"POST","/api/style/slots/"+tc.slot,map[string]any{"queue_font_size":tc.size,"show_sequence":false})
  if w.Code!=200{t.Fatalf("slot %s save: %d %s",tc.slot,w.Code,w.Body.String())}
 }
 list:=consoleRequest(t,app,"GET","/api/style/slots",nil)
 if list.Code!=200{t.Fatal(list.Code)}
 var slots []struct{Slot int `json:"slot"`;Saved bool `json:"saved"`}
 if err:=json.Unmarshal(list.Body.Bytes(),&slots);err!=nil{t.Fatal(err)}
 if len(slots)!=10||!slots[0].Saved||!slots[9].Saved||slots[1].Saved{t.Fatalf("style slots not independent: %v",slots)}
 apply:=consoleRequest(t,app,"POST","/api/style/slots/10/apply",map[string]any{})
 if apply.Code!=200||app.style["queue_font_size"]!=float64(52){t.Fatalf("slot apply: %d %s %+v",apply.Code,apply.Body.String(),app.style)}
 reloaded:=New(dir,"0.10.4","org/demo")
 if len(reloaded.styleSlots)!=2||reloaded.style["queue_font_size"]!=float64(52){t.Fatal("style slots not persisted")}
 if code:=consoleRequest(t,app,"POST","/api/style/slots/11",map[string]any{}).Code;code!=400{t.Fatalf("allowed slot 11: %d",code)}
}
func TestPermissionScopeAndAdminCapabilityBoundary(t *testing.T){
 app:=New(t.TempDir(),"0.10.4","org/demo")
 app.config.Permissions=[]PermissionEntry{
  {Name:"一纸轻予梦",Platform:"all",Role:"super_admin"},
  {Name:"Bob",Platform:"bilibili",Role:"admin",Capabilities:[]string{"queue"}},
  {Name:"Muted",Platform:"douyin",Role:"blacklist"},
  {Name:"Part",Platform:"douyin",Role:"part_time",Capabilities:[]string{"queue"}},
 }
 send:=func(platform,name,id,command string){app.OnDanmu(live.Event{Platform:platform,Username:name,UserID:id,Content:command,Time:time.Now()})}
 send("bilibili","Member","1","排队")
 if len(app.queue)!=1{t.Fatal("baseline queue")}
 send("bilibili","Bob","2","插队 1 Sneaky")
 if len(app.queue)!=1{t.Fatal("admin bypassed disabled insert permission")}
 send("bilibili","Bob","2","添加管理员 Another")
 if _,ok:=app.config.effectiveEntry(live.Event{Platform:"bilibili",Username:"Another"});ok{t.Fatal("regular admin appointed administrator")}
 send("douyin","Bob","3","删除 1")
 if len(app.queue)!=1{t.Fatal("B station admin leaked into douyin")}
 send("bilibili","Bob","2","删除 1")
 if len(app.queue)!=0{t.Fatal("admin queue permission denied")}
 send("douyin","Muted","4","排队")
 if len(app.queue)!=0{t.Fatal("blacklisted user joined")}
 send("douyin","一纸轻予梦","5","添加管理员 Mod2")
 p,ok:=app.config.effectiveEntry(live.Event{Platform:"douyin",Username:"Mod2"})
 if !ok||p.Role!="admin"{t.Fatalf("all-platform super admin failed: %+v",p)}
 send("bilibili","Mod2","6","完成") // not authorized in B station
 if len(app.queue)!=0{t.Fatal("cross-platform appointed operator leak")}
}
func TestPermissionAPIMasksNoImplicitMembers(t *testing.T){
 app:=New(t.TempDir(),"0.10.4","org/demo")
 result:=consoleRequest(t,app,"GET","/api/permissions",nil)
 if result.Code!=200{t.Fatal(result.Code)}
 var v map[string]json.RawMessage
 if err:=json.Unmarshal(result.Body.Bytes(),&v);err!=nil{t.Fatal(err)}
 if !bytes.Contains(v["entries"],[]byte("[]")){t.Fatalf("new users should not appear: %s",v["entries"])}
 w:=consoleRequest(t,app,"POST","/api/permissions",map[string]any{"entries":[]map[string]any{
  {"id":"A","platform":"all","role":"super_admin"},
  {"id":"B","platform":"bilibili","role":"admin","capabilities":[]string{"queue"}},
 }})
 if w.Code!=200{t.Fatalf("save roles: %d %s",w.Code,w.Body.String())}
 if role,ok:=app.config.effectiveEntry(live.Event{Platform:"douyin",Username:"B"});ok{t.Fatal("B station admin leaked across platform:",role)}
 if len(app.config.Permissions)!=2{t.Fatal("roles not persisted")}
 bad:=consoleRequest(t,app,"POST","/api/permissions",map[string]any{"entries":[]map[string]any{
  {"id":"A","platform":"all","role":"super_admin"},
  {"id":"a","platform":"all","role":"admin"},
 }})
 if bad.Code!=400{t.Fatalf("duplicate nick not rejected: %d",bad.Code)}
}
func TestCommandParityFromPython(t *testing.T){
 app:=New(t.TempDir(),"0.10.4","org/demo")
 words:=[]struct{command,expected string}{
  {"官服排 注释","官服"},{"B服排 备注","B服"},{"超级排队 备注","超级"},{"米服排 备注","米服"},{"排队:备注",""},
 }
 for i,v:=range words {
  app.OnDanmu(live.Event{Platform:"bilibili",Username:"User"+string(rune('A'+i)),UserID:string(rune('1'+i)),Content:v.command,Time:time.Now()})
  if len(app.queue)!=i+1||app.queue[i].Mode!=v.expected{t.Fatalf("command %s not aligned: %+v",v.command,app.queue)}
 }
 app.config.SuperAdmins=[]string{"Boss"}
 for i:=0;i<23;i++{app.queue=append(app.queue,QueueItem{Key:"manual:"+strings.Repeat("z",i+1),Platform:"manual",Username:"x"})}
 n:=len(app.queue)
 app.OnDanmu(live.Event{Platform:"bilibili",Username:"Boss",UserID:"777",Content:"无影插 21 Sneaky",Time:time.Now()})
 if len(app.queue)!=n{t.Fatal("Go accepted stealth insert above Python's 20-position ceiling")}
 app.OnDanmu(live.Event{Platform:"bilibili",Username:"Boss",UserID:"777",Content:"无影插 20 Allowed",Time:time.Now()})
 if len(app.queue)!=n+1{t.Fatal("Go rejected stealth insert within Python limit")}
}
