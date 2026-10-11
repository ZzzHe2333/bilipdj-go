package core

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "testing"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
 "github.com/ZzzHe2333/bilipdj-go/internal/storage"
)

func patchRequest(t *testing.T,a *App,method,path string,payload any)*httptest.ResponseRecorder{
 t.Helper()
 var body []byte
 if payload!=nil{
  var err error
  body,err=json.Marshal(payload)
  if err!=nil{t.Fatal(err)}
 }
 r:=httptest.NewRequest(method,"http://127.0.0.1:9816"+path,bytes.NewReader(body))
 r.RemoteAddr="127.0.0.1:9012"
 w:=httptest.NewRecorder()
 a.Routes(http.NotFoundHandler()).ServeHTTP(w,r)
 return w
}

func TestBlacklistAllScopeOutranksSpecificAdmin(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.Permissions=[]PermissionEntry{
  {Name:"SameName",Platform:"all",Role:"blacklist"},
  {Name:"SameName",Platform:"bilibili",Role:"admin",Capabilities:[]string{"insert","queue","moderate"}},
 }
 for _,platform:=range []string{"bilibili","douyin"}{
  e:=live.Event{Platform:platform,Username:"SameName",UserID:"100"}
  if !a.isBlacklisted(e)||a.commandAllowed(e,"insert")||a.isOperator(e){
   t.Fatalf("all-platform ban bypassed on %s",platform)
  }
 }
}

func TestLegacyUserIDBlacklistPersistsAfterRoleMigration(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.Blacklist=[]string{"uid-123"}
 a.config.Permissions=[]PermissionEntry{{Name:"OldNickname",Platform:"bilibili",Role:"admin",Capabilities:[]string{"queue"}}}
 e:=live.Event{Platform:"bilibili",Username:"OldNickname",UserID:"uid-123"}
 if !a.isBlacklisted(e)||a.commandAllowed(e,"queue"){t.Fatal("legacy UserID ban was lost")}
 // An arbitrary unrelated new user must not inherit the nickname ban.
 if a.isBlacklisted(live.Event{Platform:"bilibili",Username:"Other",UserID:"other-id"}) {
  t.Fatal("incorrectly blocked unrelated user")
 }
}

func TestUnbanCannotRemoveOtherPlatformOrGlobalBan(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.Permissions=[]PermissionEntry{{Name:"Victim",Platform:"all",Role:"blacklist"}}
 if a.removeRoleLocked("bilibili","Victim","blacklist"){
  t.Fatal("Bilibili action removed a global ban")
 }
 if !a.isBlacklisted(live.Event{Platform:"douyin",Username:"Victim"}){
  t.Fatal("Douyin ban was removed by Bilibili")
 }
 a.config.Permissions=append(a.config.Permissions,PermissionEntry{Name:"Victim",Platform:"bilibili",Role:"blacklist"})
 if !a.removeRoleLocked("bilibili","Victim","blacklist"){t.Fatal("specific unban failed")}
 if !a.isBlacklisted(live.Event{Platform:"bilibili",Username:"Victim"}){
  t.Fatal("global ban must remain after local unban")
 }
}

func TestPermissionTableUnbanClearsMigratedLegacyUserID(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 a.config.Blacklist=[]string{"123456"}
 a.config.Permissions=nil
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 // The new UI displays the migrated legacy row, then removes it.
 w:=patchRequest(t,a,"POST","/api/permissions",map[string]any{"entries":[]PermissionEntry{}})
 if w.Code!=200{t.Fatalf("clear legacy ban: %d %s",w.Code,w.Body.String())}
 if a.isBlacklisted(live.Event{Platform:"bilibili",Username:"Name",UserID:"123456"}) {
  t.Fatal("deleted legacy ban remains active")
 }
 reloaded:=New(dir,"test","org/demo")
 if reloaded.isBlacklisted(live.Event{Platform:"bilibili",Username:"Name",UserID:"123456"}){
  t.Fatal("deleted legacy ban reappeared after reboot")
 }
}

func TestPlatformSaveCannotOverwriteRolesOrSelectedQueueSlot(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 // A legacy/old browser first retrieves the config while slot 1 is active.
 stale:=a.config
 stale.Permissions=[]PermissionEntry{{Name:"Prior",Platform:"all",Role:"super_admin"}}
 stale.Admins=[]string{"Prior"}
 stale.Blacklist=[]string{"Forbidden"}
 stale.SuperAdmins=[]string{"Prior"}
 stale.Guards=[]string{"Prior"}
 stale.ArchiveSlot=1
 // Another page edits permissions and moves the queue to slot 2.
 a.config.Permissions=[]PermissionEntry{{Name:"NewAdmin",Platform:"douyin",Role:"admin",Capabilities:[]string{"queue"}}}
 a.config.Admins=[]string{"NewAdmin"}
 a.config.SuperAdmins=[]string{}
 a.config.Guards=[]string{}
 a.config.Blacklist=[]string{"user-id-999"}
 a.switchSlotLocked(2)
 a.queue=[]QueueItem{{Key:"bilibili:11",Username:"QueueOwner",Platform:"bilibili"}}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 stale.Command="join" // Legitimate unrelated settings change must still apply.
 w:=patchRequest(t,a,"POST","/api/config",stale)
 if w.Code!=200{t.Fatalf("save stale config failed: %d %s",w.Code,w.Body.String())}
 if a.config.Command!="join"||a.config.ArchiveSlot!=2{t.Fatalf("new config or slot overwritten: %+v",a.config)}
 if len(a.queue)!=1||a.queue[0].Key!="bilibili:11"{t.Fatalf("selected queue overwritten: %+v",a.queue)}
 if !reflect.DeepEqual(a.config.Permissions,[]PermissionEntry{{Name:"NewAdmin",Platform:"douyin",Role:"admin",Capabilities:[]string{"queue"}}}){
  t.Fatalf("new permissions overwritten: %+v",a.config.Permissions)
 }
 if !reflect.DeepEqual(a.config.Blacklist,[]string{"user-id-999"})||
  !reflect.DeepEqual(a.config.Admins,[]string{"NewAdmin"})||
  len(a.config.SuperAdmins)!=0||len(a.config.Guards)!=0{
  t.Fatal("legacy role mirrors overwritten")
 }
 after:=New(dir,"test","org/demo")
 if after.config.ArchiveSlot!=2||after.config.Permissions[0].Name!="NewAdmin"||len(after.queue)!=1{
  t.Fatal("stale config overwrote persisted permissions or archive slot")
 }
}

func TestStyleApplyFailureRollsBackDiskAndMemory(t *testing.T){
 for _,tc:=range []struct{method,path string}{
  {"slot","/api/style/slots/1/apply"},
  {"direct","/api/style"},
 }{
  t.Run(tc.method,func(t *testing.T){
   dir:=t.TempDir()
   a:=New(dir,"test","org/demo")
   if err:=a.SetStoragePlan(storage.Plan{Mode:"managed",Active:dir});err!=nil{t.Fatal(err)}
   previous:=a.style["text_color"]
   if err:=a.saveLocked();err!=nil{t.Fatal(err)}
   // Existing directory is intentionally not a regular appearance file.
   if err:=os.Mkdir(filepath.Join(dir,"style-web.json"),0700);err!=nil{t.Fatal(err)}
   desired:=map[string]any{"text_color":"#ff9900"}
   var response *httptest.ResponseRecorder
   if tc.method=="slot"{
    if x:=patchRequest(t,a,"POST","/api/style/slots/1",desired);x.Code!=200{t.Fatalf("save preset: %d",x.Code)}
    response=patchRequest(t,a,"POST",tc.path,map[string]any{})
   }else{
    response=patchRequest(t,a,"POST",tc.path,desired)
   }
   if response.Code!=500{t.Fatalf("expected write failure; got %d %s",response.Code,response.Body.String())}
   if a.style["text_color"]!=previous{t.Fatal("failed style changed current in-memory style")}
   reloaded:=New(dir,"test","org/demo")
   if reloaded.style["text_color"]!=previous{t.Fatal("failed style persisted into state.json")}
  })
 }
}
