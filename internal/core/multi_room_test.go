package core

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func extraRoom(id, platform, room, cookie string, on bool)ListenerConfig{
 return ListenerConfig{ID:id,Platform:platform,Room:room,Cookie:cookie,Enabled:on}
}

func TestSamePlatformRoomCookieValidation(t *testing.T){
 c:=defaultConfig()
 c.Bilibili=PlatformConfig{Enabled:true,Room:"123"} // legacy single room stays compatible
 if _,err:=cleanConfig(c);err!=nil{t.Fatalf("legacy B station: %v",err)}
 c.Listeners=[]ListenerConfig{extraRoom("bilibili-room-two","bilibili","456","SESSDATA=two; DedeUserID=10",true)}
 if _,err:=cleanConfig(c);err==nil{t.Fatal("multi B without primary cookie accepted")}
 c.Bilibili.Cookie="SESSDATA=one; DedeUserID=10"
 if _,err:=cleanConfig(c);err!=nil{t.Fatal(err)}
 c.Listeners[0].Cookie="SESSDATA=one; DedeUserID=123"
 if _,err:=cleanConfig(c);err==nil{t.Fatal("same SESSDATA reused across different rooms")}
 c.Listeners[0].Cookie="SESSDATA=two"
 c.Listeners[0].Room="123"
 if _,err:=cleanConfig(c);err==nil{t.Fatal("same B room double monitored")}
 c.Listeners[0].Room="456"
 c.Listeners=append(c.Listeners,extraRoom("douyin-room-one","douyin","789","",true))
 c.Listeners=append(c.Listeners,extraRoom("douyin-room-two","douyin","987","",true))
 if _,err:=cleanConfig(c);err!=nil{t.Fatalf("two douyin allowed: %v",err)}
 c.Listeners[1].ID="bilibili-room-two"
 if _,err:=cleanConfig(c);err==nil{t.Fatal("duplicate/incorrect instance ID accepted")}
}

func TestRoomConfigSecretsAndImmutableCookieIDs(t *testing.T) {
 a:=New(t.TempDir(),"0.10.4","org/repo")
 a.config.Bilibili.Cookie="SESSDATA=primary-secret"
 a.config.Listeners=[]ListenerConfig{
  extraRoom("bilibili-room-one","bilibili","123","SESSDATA=room-one-secret",false),
  extraRoom("bilibili-room-two","bilibili","456","SESSDATA=room-two-secret",false),
 }
 handler:=a.Routes(http.NotFoundHandler())
 request:=func(method string,body []byte)*httptest.ResponseRecorder {
  t.Helper()
  req:=httptest.NewRequest(method,"http://127.0.0.1:9816/api/config",bytes.NewReader(body))
  req.RemoteAddr="127.0.0.1:50000"
  res:=httptest.NewRecorder()
  handler.ServeHTTP(res,req)
  return res
 }
 get:=request("GET",nil)
 if get.Code!=200||bytes.Contains(get.Body.Bytes(),[]byte("secret")){t.Fatalf("API leaked credentials: %d %s",get.Code,get.Body.String())}
 var payload struct {
  Config Config `json:"config"`
  ListenerCookies map[string]bool `json:"listener_cookies"`
 }
 if err:=json.Unmarshal(get.Body.Bytes(),&payload);err!=nil{t.Fatal(err)}
 if !payload.ListenerCookies["bilibili-room-one"]||!payload.ListenerCookies["bilibili-room-two"]{t.Fatal("missing cookie state")}
 if payload.Config.Listeners[0].Cookie!=""{t.Fatal("cookie leaked into config JSON")}
 // Reorder the list; each cookie must remain bound by ID.
 cfg:=payload.Config
 cfg.Listeners[0],cfg.Listeners[1]=cfg.Listeners[1],cfg.Listeners[0]
 data,_:=json.Marshal(cfg)
 result:=request("POST",data)
 if result.Code!=200{t.Fatalf("save: %d %s",result.Code,result.Body.String())}
 byID:=map[string]string{}
 for _,p:=range a.config.Listeners{byID[p.ID]=p.Cookie}
 if byID["bilibili-room-one"]!="SESSDATA=room-one-secret"||byID["bilibili-room-two"]!="SESSDATA=room-two-secret"{t.Fatal("cookie was reassigned to wrong room")}
 // A legacy client omitting the new field must not delete extra rooms.
 legacy:=cfg
 legacy.Listeners=nil
 data,_=json.Marshal(legacy)
 result=request("POST",data)
 if result.Code!=200||len(a.config.Listeners)!=2{t.Fatalf("older config client unexpectedly removed rooms: %d",result.Code)}
 cfg.Listeners=cfg.Listeners[:1]
 data,_=json.Marshal(cfg)
 result=request("POST",data)
 if result.Code!=200{t.Fatalf("remove room: %d %s",result.Code,result.Body.String())}
 if len(a.config.Listeners)!=1||a.config.Listeners[0].ID!="bilibili-room-two"{t.Fatal("removed room credential was retained")}
 reload:=New(strings.TrimSuffix(a.dataPath,"/state.json"),"0.10.4","org/repo")
 if len(reload.config.Listeners)!=1{t.Fatal("reload lost extra room")}
}

type fakeRoomSource struct {
 mu sync.Mutex
 connections []string
 canceled []string
}
func (f *fakeRoomSource) Run(ctx context.Context, room, cookie string,emit live.Emit,report live.Report){
 f.mu.Lock()
 f.connections=append(f.connections,room+"|"+cookie)
 f.mu.Unlock()
 report(live.Status{Connected:true,Message:"connected",Since:time.Now()})
 emit(live.Event{UserID:"42",Username:"viewer",Content:"排队",Time:time.Now()})
 <-ctx.Done()
 f.mu.Lock()
 f.canceled=append(f.canceled,room)
 f.mu.Unlock()
}
func TestMultipleWorkersShareQueueAndStatusKeys(t *testing.T){
 a:=New(t.TempDir(),"0.10.4","org/repo")
 source:=&fakeRoomSource{}
 a.listenerFactory=func(string)live.Source{return source}
 cfg:=defaultConfig()
 cfg.Bilibili=PlatformConfig{Enabled:true,Room:"123",Cookie:"SESSDATA=a"}
 cfg.Douyin=PlatformConfig{Enabled:true,Room:"789"}
 cfg.Listeners=[]ListenerConfig{extraRoom("bilibili-room-two","bilibili","456","SESSDATA=b",true),extraRoom("douyin-room-two","douyin","987","",true)}
 cleaned,err:=cleanConfig(cfg);if err!=nil{t.Fatal(err)}
 a.config=cleaned
 a.Start()
 defer a.Stop()
 deadline:=time.Now().Add(2*time.Second)
 for time.Now().Before(deadline){
  a.mu.RLock()
  count:=len(a.queue)
  statusCount:=0
  for _,s:=range a.statuses{if s.Connected{statusCount++}}
  a.mu.RUnlock()
  if count==2&&statusCount==4{break}
  time.Sleep(10*time.Millisecond)
 }
 a.mu.RLock()
 defer a.mu.RUnlock()
 if len(a.queue)!=2{t.Fatalf("expected B and Douyin users once each, got %+v",a.queue)}
 if len(a.statuses)!=4{t.Fatalf("expected four distinct instance statuses, got %+v",a.statuses)}
 if len(a.workers)!=4{t.Fatalf("expected independent workers, got %d",len(a.workers))}
 if a.queue[0].Platform==a.queue[1].Platform{t.Fatal("distinct platforms not kept distinct")}
}

// Refreshing a single credential must NOT cancel all of the other streams.
func TestTargetedListenerRefreshDoesNotReconnectOtherRooms(t *testing.T){
 app:=New(t.TempDir(),"0.10.4","org/repo")
 source:=&fakeRoomSource{}
 app.listenerFactory=func(string)live.Source{return source}
 c:=defaultConfig()
 c.Bilibili=PlatformConfig{Enabled:true,Room:"123",Cookie:"SESSDATA=abc"}
 c.Listeners=[]ListenerConfig{extraRoom("bilibili-second","bilibili","456","SESSDATA=def",true)}
 app.config=c
 app.Start()
 defer app.Stop()
 limit:=time.Now().Add(time.Second)
 for time.Now().Before(limit){
  source.mu.Lock();count:=len(source.connections);source.mu.Unlock()
  if count==2{break};time.Sleep(5*time.Millisecond)
 }
 app.refreshListener("bilibili-second")
 limit=time.Now().Add(time.Second)
 for time.Now().Before(limit){
  source.mu.Lock();runs:=len(source.connections);stopped:=len(source.canceled);source.mu.Unlock()
  if runs==3&&stopped==1{break};time.Sleep(5*time.Millisecond)
 }
 source.mu.Lock()
 defer source.mu.Unlock()
 if len(source.connections)!=3{t.Fatalf("expected three total runs, got %v",source.connections)}
 if len(source.canceled)!=1||source.canceled[0]!="456"{t.Fatalf("unrelated room was disconnected: %v",source.canceled)}
}

func TestActivePlatformsReturnUniqueNamesAndListenerInstances(t *testing.T){
 a:=New(t.TempDir(),"0.10.4","org/repo")
 a.config.Bilibili=PlatformConfig{Enabled:true,Room:"123",Cookie:"SESSDATA=abc"}
 a.config.Listeners=[]ListenerConfig{
  extraRoom("bilibili-extra-one","bilibili","456","SESSDATA=def",true),
  extraRoom("douyin-extra-one","douyin","789","",true),
 }
 req:=httptest.NewRequest(http.MethodGet,"http://127.0.0.1:9816/api/platforms/active",nil)
 res:=httptest.NewRecorder()
 a.Routes(http.NotFoundHandler()).ServeHTTP(res,req)
 if res.Code!=200{t.Fatalf("status: %d",res.Code)}
 var payload struct {
  Active []string `json:"active"`
  Instances []string `json:"instances"`
 }
 if err:=json.Unmarshal(res.Body.Bytes(),&payload);err!=nil{t.Fatal(err)}
 if len(payload.Active)!=2||payload.Active[0]!="bilibili"||payload.Active[1]!="douyin"{t.Fatalf("platforms not deduplicated: %v",payload.Active)}
 if len(payload.Instances)!=3{t.Fatalf("missing room IDs: %v",payload.Instances)}
}
