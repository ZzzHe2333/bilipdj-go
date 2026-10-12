package core

import (
 "context"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
 "github.com/ZzzHe2333/bilipdj-go/internal/storage"
)

type controllableConnection struct {
 ctx context.Context
 emit live.Emit
 report live.Report
}
type controllableRoomSource struct {
 started chan controllableConnection
}
func (s *controllableRoomSource) Run(ctx context.Context,_ string,_ string,emit live.Emit,report live.Report){
 s.started<-controllableConnection{ctx:ctx,emit:emit,report:report}
 <-ctx.Done()
}
func waitConnection(t *testing.T,s *controllableRoomSource)controllableConnection{
 t.Helper()
 select{
 case c:=<-s.started:return c
 case <-time.After(3*time.Second):t.Fatal("listener did not start")
 }
 return controllableConnection{}
}

func TestTargetedReconnectRejectsLateOldRoomEventsAndStatuses(t *testing.T){
 a:=New(t.TempDir(),"test","repo")
 source:=&controllableRoomSource{started:make(chan controllableConnection,4)}
 a.listenerFactory=func(string)live.Source{return source}
 a.config.Bilibili=PlatformConfig{Enabled:true,Room:"123",Cookie:"SESSDATA=abc"}
 a.Start()
 defer a.Stop()
 old:=waitConnection(t,source)
 a.refreshListener("bilibili")
 current:=waitConnection(t,source)
 if old.ctx.Err()==nil||current.ctx.Err()!=nil{t.Fatal("targeted refresh did not cancel the previous worker")}
 old.emit(live.Event{Platform:"bilibili",UserID:"stale",Username:"Stale",Content:"排队"})
 old.report(live.Status{Connected:false,Message:"OLD DISCONNECTED"})
 a.mu.RLock()
 n:=len(a.queue)
 status:=a.statuses["bilibili"]
 a.mu.RUnlock()
 if n!=0||status.Message=="OLD DISCONNECTED"{
  t.Fatalf("canceled room contaminated new connection, queue=%d, status=%+v",n,status)
 }
 current.report(live.Status{Connected:true,Message:"NEW CONNECTED"})
 current.emit(live.Event{Platform:"bilibili",UserID:"new",Username:"New",Content:"排队"})
 a.mu.RLock()
 n=len(a.queue)
 status=a.statuses["bilibili"]
 a.mu.RUnlock()
 if n!=1||status.Message!="NEW CONNECTED"||!status.Connected{
  t.Fatalf("current connection callbacks rejected: queue=%d status=%+v",n,status)
 }
 a.Stop()
 current.emit(live.Event{Platform:"bilibili",UserID:"stopped",Username:"Stopped",Content:"排队"})
 a.mu.RLock()
 n=len(a.queue)
 a.mu.RUnlock()
 if n!=1{t.Fatal("stopped listener still appended a queue item")}
}

func TestPythonCSVImportStateWriteFailureRollsBackLocalAndMemory(t *testing.T){
 root:=t.TempDir()
 dataDir:=filepath.Join(root,"roaming","bilipdj-go")
 localDir:=filepath.Join(root,"local","bilipdj-go","archives")
 pythonDir:=filepath.Join(root,"local","bilipdj","archives")
 if err:=os.MkdirAll(pythonDir,0700);err!=nil{t.Fatal(err)}
 csvPath:=filepath.Join(pythonDir,"queue_archive_slot_1.csv")
 csvData:=[]byte("\ufeff序号,id,内容,最后操作时间,来源平台\n1,Imported,,2026-10-09T15:00:00,douyin\n")
 if err:=os.WriteFile(csvPath,csvData,0600);err!=nil{t.Fatal(err)}
 a:=New(dataDir,"test","repo")
 a.queue=[]QueueItem{{Key:"manual:old",Platform:"manual",Username:"Old"}}
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 plan:=storage.Plan{Mode:"managed",Active:dataDir,User:dataDir,ArchiveDir:localDir,PythonArchiveDir:pythonDir,BackupDir:filepath.Join(root,"backups")}
 if err:=a.SetStoragePlan(plan);err!=nil{t.Fatal(err)}
 before:=a.queueSnapshotLocked()
 unblock:=blockStateFile(t,a)
 req:=httptest.NewRequest(http.MethodPost,"http://localhost/api/storage/python-queues/import",strings.NewReader("{}"))
 req.RemoteAddr="127.0.0.1:4242"
 w:=httptest.NewRecorder()
 a.Routes(http.NotFoundHandler()).ServeHTTP(w,req)
 if w.Code!=500{t.Fatalf("state write should fail: %d %s",w.Code,w.Body.String())}
 if !equalSnapshot(a.queueSnapshotLocked(),before){t.Fatalf("failed import changed in-memory queue: %+v",a.queue)}
 unblock()
 reloaded:=New(dataDir,"test","repo")
 if err:=reloaded.SetStoragePlan(plan);err!=nil{t.Fatal(err)}
 if !equalSnapshot(reloaded.queueSnapshotLocked(),before){
  t.Fatalf("failed import changed persisted Local queue: %+v",reloaded.queue)
 }
 after,err:=os.ReadFile(csvPath)
 if err!=nil||string(after)!=string(csvData){t.Fatal("import changed Python source CSV")}
}
