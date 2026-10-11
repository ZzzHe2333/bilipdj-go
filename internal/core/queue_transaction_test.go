package core

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "sync"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// Occupying state.json with a directory makes the final atomic os.Rename fail,
// after all application-level validations have passed.
func blockStateFile(t *testing.T,a *App) func(){
 t.Helper()
 old:=a.dataPath+".before-test"
 if e:=os.Rename(a.dataPath,old);e!=nil{t.Fatal(e)}
 if e:=os.Mkdir(a.dataPath,0700);e!=nil{t.Fatal(e)}
 return func(){
  t.Helper()
  if e:=os.Remove(a.dataPath);e!=nil{t.Fatal(e)}
  if e:=os.Rename(old,a.dataPath);e!=nil{t.Fatal(e)}
 }
}
func statePost(t *testing.T,a *App,endpoint string,body any)*httptest.ResponseRecorder{
 t.Helper()
 return consoleRequest(t,a,http.MethodPost,endpoint,body)
}
func equalSnapshot(a,b queueSnapshot)bool{
 return a.active==b.active&&reflect.DeepEqual(a.queue,b.queue)&&reflect.DeepEqual(a.slots,b.slots)
}
func TestConfigSaveFailureLeavesRuntimeAndDiskUnchanged(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 old:=a.config
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 unblock:=blockStateFile(t,a)
 desired:=a.config
 desired.AutoQueue=!old.AutoQueue
 desired.Command="新版命令"
 desired.Bilibili.Room="301"
 desired.Bilibili.Enabled=true
 desired.Bilibili.Cookie="SESSDATA=valid"
 resp:=statePost(t,a,"/api/config",desired)
 if resp.Code!=500{t.Fatalf("expected save failure, got %d %s",resp.Code,resp.Body.String())}
 if !reflect.DeepEqual(a.config,old){t.Fatalf("save failure mutated running config: %+v",a.config)}
 if len(a.workers)!=0{t.Fatalf("failed config unexpectedly started workers: %d",len(a.workers))}
 unblock()
 reloaded:=New(dir,"test","org/demo")
 if !reflect.DeepEqual(reloaded.config,old){t.Fatal("failure changed persisted configuration")}
}

func TestArchiveSwitchFailureRestoresQueueAndAllSlots(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 a.queue=[]QueueItem{{Key:"manual:a",Username:"QueueA",Platform:"manual"}}
 a.slots["2"]=[]QueueItem{{Key:"manual:b",Username:"QueueB",Platform:"manual"}}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 before:=a.queueSnapshotLocked()
 unblock:=blockStateFile(t,a)
 response:=statePost(t,a,"/api/queue/slots",map[string]any{"slot":2})
 if response.Code!=500{t.Fatalf("expected 500, got %d %s",response.Code,response.Body.String())}
 if !equalSnapshot(a.queueSnapshotLocked(),before){t.Fatal("archive switch failure corrupted in-memory queue or slots")}
 unblock()
 reloaded:=New(dir,"test","org/demo")
 if !equalSnapshot(reloaded.queueSnapshotLocked(),before){t.Fatal("archive switch failure changed saved archive")}
}

func TestAllQueueMutationsRejectMissingOrStaleSlots(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.queue=[]QueueItem{{Key:"manual:a",Username:"A",Platform:"manual"},{Key:"manual:b",Username:"B",Platform:"manual"}}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 cases:=[]map[string]any{
  {"action":"add","name":"Other"},
  {"action":"insert","name":"Other","index":0},
  {"action":"remove","key":"manual:a"},
  {"action":"move","key":"manual:a","index":1},
  {"action":"edit","key":"manual:a","note":"bad"},
  {"action":"clear"},
  {"action":"reorder","keys":[]string{"manual:b","manual:a"},"before":[]string{"manual:a","manual:b"}},
 }
 for _,tc:=range cases{
  before:=a.queueSnapshotLocked()
  result:=statePost(t,a,"/api/queue",tc)
  if result.Code!=400{t.Fatalf("missing slot on %v: %d %s",tc,result.Code,result.Body.String())}
  tc["slot"]=2
  result=statePost(t,a,"/api/queue",tc)
  if result.Code!=409{t.Fatalf("wrong slot on %v: %d %s",tc,result.Code,result.Body.String())}
  if !equalSnapshot(a.queueSnapshotLocked(),before){t.Fatalf("rejected request modified queue: %+v",tc)}
 }
}

func TestAllQueueMutationsRollbackWhenStorageFails(t *testing.T){
 cases:=[]map[string]any{
  {"action":"add","name":"Extra","slot":1},
  {"action":"insert","name":"Extra","index":0,"slot":1},
  {"action":"remove","key":"manual:a","slot":1},
  {"action":"move","key":"manual:a","index":1,"slot":1},
  {"action":"edit","key":"manual:a","note":"updated","slot":1},
  {"action":"clear","slot":1},
  {"action":"reorder","keys":[]string{"manual:b","manual:a"},"before":[]string{"manual:a","manual:b"},"slot":1},
 }
 for _,tc:=range cases{
  t.Run(tc["action"].(string),func(t *testing.T){
   dir:=t.TempDir()
   a:=New(dir,"test","org/demo")
   a.queue=[]QueueItem{{Key:"manual:a",Username:"A",Platform:"manual"},{Key:"manual:b",Username:"B",Platform:"manual"}}
   if err:=a.saveLocked();err!=nil{t.Fatal(err)}
   before:=a.queueSnapshotLocked()
   unblock:=blockStateFile(t,a)
   response:=statePost(t,a,"/api/queue",tc)
   if response.Code!=500{t.Fatalf("expected failing storage 500, got %d %s",response.Code,response.Body.String())}
   if !equalSnapshot(a.queueSnapshotLocked(),before){t.Fatalf("failed %s changed running queue",tc["action"])}
   unblock()
   reloaded:=New(dir,"test","org/demo")
   if !equalSnapshot(reloaded.queueSnapshotLocked(),before){t.Fatalf("failed %s persisted queue change",tc["action"])}
  })
 }
}

func TestConfigRequiresAdminForRemoteReads(t *testing.T){
 a:=New(t.TempDir(),"test","org/demo")
 a.config.Blacklist=[]string{"person-not-public"}
 h:=a.Routes(http.NotFoundHandler())
 makeRequest:=func(remote,token string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(http.MethodGet,"http://127.0.0.1:9816/api/config",nil)
  req.RemoteAddr=remote
  if token!=""{req.Header.Set("X-Admin-Token",token)}
  w:=httptest.NewRecorder()
  h.ServeHTTP(w,req)
  return w
 }
 if w:=makeRequest("192.0.2.1:1111","");w.Code!=403{
  t.Fatalf("remote GET config status %d; should be 403",w.Code)
 }
 t.Setenv("BILIPDJ_ADMIN_TOKEN","valid-secret")
 w:=makeRequest("192.0.2.1:1111","valid-secret")
 if w.Code!=200{t.Fatalf("valid admin token rejected: %d %s",w.Code,w.Body.String())}
 var response struct{Config Config `json:"config"`}
 if err:=json.Unmarshal(w.Body.Bytes(),&response);err!=nil{t.Fatal(err)}
 if len(response.Config.Blacklist)!=1{t.Fatal("admin could not read config")}
}

// Concurrent requests must leave running workers in the exact same state as
// the last successfully persisted config; a stale request cannot apply later.
func TestConcurrentConfigSavesCannotRestartStaleWorkers(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","org/demo")
 var wg sync.WaitGroup
 start:=make(chan struct{})
 results:=make(chan int,2)
 for _,enabled:=range []bool{true,false}{
  wg.Add(1)
  go func(enabled bool){
   defer wg.Done();<-start
   cfg:=defaultConfig()
   cfg.Bilibili.Enabled=enabled
   cfg.Bilibili.Room="301"
   cfg.Bilibili.Cookie="SESSDATA=one"
   result:=statePost(t,a,"/api/config",cfg)
   results<-result.Code
  }(enabled)
 }
 close(start);wg.Wait();close(results)
 for status:=range results{if status!=200{t.Fatalf("config save returned %d",status)}}
 a.mu.RLock()
 current:=a.config.Bilibili.Enabled
 _,workerActive:=a.workers["bilibili"]
 status:=a.statuses["bilibili"]
 a.mu.RUnlock()
 if current!=workerActive||status.Message=="未启用"&&current{
  t.Fatalf("workers are stale: enabled=%v worker=%v status=%+v",current,workerActive,status)
 }
 reloaded:=New(dir,"test","org/demo")
 if reloaded.config.Bilibili.Enabled!=current{
  t.Fatalf("persisted config differed from running worker configuration")
 }
 a.Stop()
 _=time.Now()
 _=live.Event{}
}
