package core

import (
 "bytes"
 "mime/multipart"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
 "github.com/ZzzHe2333/bilipdj-go/internal/storage"
)

func TestDanmuSaveFailureRollsBackQueueAndDailyCounts(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 a.config.DailyQueueLimit=2
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 before,err:=a.liveSnapshotLocked()
 if err!=nil{t.Fatal(err)}
 unblock:=blockStateFile(t,a)
 a.OnDanmu(live.Event{Platform:"bilibili",Username:"Viewer",UserID:"100",Content:"排队",Time:time.Now()})
 if !equalSnapshot(a.queueSnapshotLocked(),before.queue) {t.Fatal("failed danmu modified queue")}
 if !reflect.DeepEqual(a.dailyCounts,before.dailyCounts)||a.dailyPeriod!=before.dailyPeriod{
  t.Fatal("daily quota was spent despite failed persistence")
 }
 unblock()
 reloaded:=New(dir,"test","org/demo")
 if len(reloaded.queue)!=0||len(reloaded.dailyCounts)!=0{t.Fatal("failed danmu persisted unexpectedly")}
 a.OnDanmu(live.Event{Platform:"bilibili",Username:"Viewer",UserID:"100",Content:"排队",Time:time.Now()})
 if len(a.queue)!=1||a.dailyCounts["bilibili:100"]!=1{t.Fatal("user cannot retry after storage recovered")}
}

func TestDanmuModeratorSwitchRollbackOnWriteFailure(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.SuperAdmins=[]string{"Boss"}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 unblock:=blockStateFile(t,a)
 a.OnDanmu(live.Event{Platform:"bilibili",Username:"Boss",UserID:"42",Content:"暂停排队功能",Time:time.Now()})
 if !a.config.Switches.Paidui{t.Fatal("failed command disabled queue switch in memory")}
 unblock()
 if !New(filepath.Dir(a.dataPath),"test","org/demo").config.Switches.Paidui{t.Fatal("failed command persisted")}
}

func TestStructuredBlacklistPreventsGiftCredit(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.GiftQueue=&GiftSettings{Enabled:true,Names:[]string{"小花花"},SlotsPerGift:2}
 a.config.Permissions=[]PermissionEntry{
  {Name:"Alice",Platform:"all",Role:"blacklist"},
  {Name:"Alice",Platform:"bilibili",Role:"admin",Capabilities:[]string{"queue"}},
 }
 a.OnDanmu(giftEvent("123","小花花","gift-blocked-1","gold"))
 if a.giftCredits["bilibili:123"]!=0{t.Fatal("globally banned user earned gift points")}
 a.config.Permissions=[]PermissionEntry{{Name:"Alice",Platform:"bilibili",Role:"blacklist"}}
 a.OnDanmu(giftEvent("123","小花花","gift-blocked-2","gold"))
 if a.giftCredits["bilibili:123"]!=0{t.Fatal("platform banned user earned gift points")}
 a.config.Permissions=[]PermissionEntry{}
 a.config.Blacklist=[]string{"123"}
 a.OnDanmu(giftEvent("123","小花花","gift-blocked-3","gold"))
 if a.giftCredits["bilibili:123"]!=0{t.Fatal("legacy ID banned user earned gift points")}
}

func TestGiftSaveFailureDoesNotGrantOrPublishSuccess(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.GiftQueue=&GiftSettings{Enabled:true,Names:[]string{"小花花"},SlotsPerGift:2}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 ch:=make(chan Event,8)
 a.subscribers[ch]=struct{}{}
 unblock:=blockStateFile(t,a)
 a.OnDanmu(giftEvent("123","小花花","gift-failed-1","gold"))
 if len(a.giftCredits)!=0||len(a.giftSeen)!=0||a.giftLast!=nil{t.Fatal("gift save failure left in-memory entitlement")}
 for len(ch)>0{
  ev:=<-ch
  if ev.Type=="gift"{t.Fatal("published gift success before disk commit")}
 }
 unblock()
 a.OnDanmu(giftEvent("123","小花花","gift-failed-1","gold"))
 if a.giftCredits["bilibili:123"]!=2{t.Fatal("replayed gift did not grant after storage recovery")}
}

func TestLegacyImportFailureRestoresSeparateLocalArchive(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 a.queue=[]QueueItem{{Key:"manual:keep",Username:"Keep",Platform:"manual"}}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 plan:=storage.Plan{Mode:"managed",User:dir,Active:dir,ArchiveDir:filepath.Join(dir,"Local","archives"),BackupDir:filepath.Join(dir,"Local","backups")}
 if err:=a.SetStoragePlan(plan);err!=nil{t.Fatal(err)}
 before:=a.queueSnapshotLocked()
 beforeConfig:=a.config.Command
 raw:=legacyZip(t)
 body:=new(bytes.Buffer)
 mp:=multipart.NewWriter(body)
 part,err:=mp.CreateFormFile("file","old.zip")
 if err!=nil{t.Fatal(err)}
 if _,err=part.Write(raw);err!=nil{t.Fatal(err)}
 if err=mp.Close();err!=nil{t.Fatal(err)}
 unblock:=blockStateFile(t,a)
 request:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:9816/api/legacy/import",body)
 request.Header.Set("Content-Type",mp.FormDataContentType())
 request.RemoteAddr="127.0.0.1:12345"
 response:=httptest.NewRecorder()
 a.Routes(http.NotFoundHandler()).ServeHTTP(response,request)
 if response.Code!=500{t.Fatalf("expected import failure, got %d %s",response.Code,response.Body.String())}
 if !equalSnapshot(a.queueSnapshotLocked(),before)||a.config.Command!=beforeConfig{
  t.Fatal("failed import changed queue or config")
 }
 unblock()
 reloaded:=New(dir,"test","org/demo")
 if err:=reloaded.SetStoragePlan(plan);err!=nil{t.Fatal(err)}
 if !equalSnapshot(reloaded.queueSnapshotLocked(),before){t.Fatalf("Local file was not rolled back: %+v",reloaded.queue)}
}

func TestCredentialCommitRebasesNewQRLogin(t *testing.T){
 previous:=defaultConfig()
 previous.Bilibili.Cookie="SESSDATA=before"
 previous.Listeners=[]ListenerConfig{{ID:"bilibili-extra-123",Platform:"bilibili",Room:"456",Cookie:"SESSDATA=old-extra"}}
 submitted:=previous
 submitted.Listeners=append([]ListenerConfig(nil),previous.Listeners...)
 submitted.Bilibili.Cookie=""
 submitted.Listeners[0].Cookie=""
 flags:=explicitCredentialFields(submitted)
 mergeCurrentCredentials(&submitted,previous,flags)
 // QR login finishes while cleanConfig is validating the submitted form.
 latest:=previous
 latest.Bilibili.Cookie="SESSDATA=new-login"
 latest.Listeners=append([]ListenerConfig(nil),previous.Listeners...)
 latest.Listeners[0].Cookie="SESSDATA=new-extra"
 mergeCurrentCredentials(&submitted,latest,flags)
 if submitted.Bilibili.Cookie!=latest.Bilibili.Cookie||submitted.Listeners[0].Cookie!=latest.Listeners[0].Cookie{
  t.Fatal("old settings form replaced new QR credentials")
 }
 // Explicit manual credentials are still intentionally honored.
 submitted.Bilibili.Cookie="SESSDATA=typed"
 flags["bilibili"]=true
 mergeCurrentCredentials(&submitted,latest,flags)
 if submitted.Bilibili.Cookie!="SESSDATA=typed"{t.Fatal("explicit cookie unexpectedly overwritten")}
 // An old room's credential must never leak to a NEW instance ID.
 submitted.Listeners=append(submitted.Listeners,ListenerConfig{ID:"bilibili-extra-999",Platform:"bilibili",Room:"777"})
 mergeCurrentCredentials(&submitted,latest,flags)
 if submitted.Listeners[1].Cookie!=""{t.Fatal("cookie inherited by a different room")}
}

func TestLiveSnapshotDeepCopiesPermissionMutations(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.Permissions=[]PermissionEntry{{Name:"Leader",Platform:"all",Role:"super_admin"}}
 before,err:=a.liveSnapshotLocked()
 if err!=nil{t.Fatal(err)}
 a.config.Permissions[0].Role="blacklist"
 a.config.Switches.Paidui=false
 if err:=a.restoreLiveLocked(before);err!=nil{t.Fatal(err)}
 if a.config.Permissions[0].Role!="super_admin"||!a.config.Switches.Paidui{
  t.Fatal("snapshot did not isolate pointers and permission slices")
 }
}
