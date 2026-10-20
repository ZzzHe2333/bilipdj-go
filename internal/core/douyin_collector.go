package core

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

type douyinCollectorProcess struct {
 mu sync.Mutex
 cmd *exec.Cmd
 mode string
 startedAt time.Time
 stoppedAt time.Time
 lastError string
 debugPort int
}
func newDouyinCollectorProcess() *douyinCollectorProcess {return &douyinCollectorProcess{}}

func (a *App) SetDouyinCollectorListen(listen string) {
 _, port, err := net.SplitHostPort(listen)
 if err != nil {return}
 n, err := strconv.Atoi(port)
 if err == nil && n > 0 && n < 65536 {a.douyinCollectorPort=n}
}
func (a *App) collectorDirectory() string {
 return filepath.Join(filepath.Dir(a.dataPath),"douyin-collector")
}
func (a *App) collectorScript() (string,error) {
 exe,err:=os.Executable()
 if err != nil{return "",err}
 candidates:=[]string{
  filepath.Join(filepath.Dir(exe),"tools","douyin-gift-collector","collector.mjs"),
  filepath.Join("tools","douyin-gift-collector","collector.mjs"),
 }
 for _,item:=range candidates {
  abs,e:=filepath.Abs(item)
  if e==nil{if info,e:=os.Stat(abs);e==nil&&info.Mode().IsRegular(){return abs,nil}}
 }
 return "",errors.New("未找到 tools/douyin-gift-collector/collector.mjs；请使用完整程序包")
}
func collectorFreePort() (int,error) {
 ln,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{return 0,err}
 _,port,_:=net.SplitHostPort(ln.Addr().String())
 _=ln.Close()
 return strconv.Atoi(port)
}
func (a *App) collectorRooms() ([]string,error) {
 a.mu.RLock()
 defer a.mu.RUnlock()
 rooms:=make([]string,0)
 for _,pc:=range configuredListeners(a.config) {
  if pc.Platform!="douyin"||!pc.Enabled{continue}
  rid,err:=live.ValidateDouyinRoom(pc.Room)
  if err!=nil{return nil,fmt.Errorf("%s: %w",pc.ID,err)}
  rooms=append(rooms,rid+":"+pc.ID)
 }
 if len(rooms)==0{return nil,errors.New("请先保存并启用至少一个抖音直播间")}
 return rooms,nil
}
func (a *App) startDouyinCollector(mode string) error {
 if mode!="login"&&mode!="run"{return errors.New("无效动作")}
 node,err:=exec.LookPath("node")
 if err!=nil{return errors.New("需要安装 Node.js 22+ 并加入 PATH")}
 versionBytes,versionErr:=exec.Command(node,"--version").Output()
 if versionErr!=nil{return errors.New("无法获取 Node.js 版本")}
 major,parseErr:=strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(string(versionBytes)),"v"),".",2)[0])
 if parseErr!=nil||major<22{return fmt.Errorf("抖音无依赖浏览器采集器需要 Node.js 22+（当前 %s）",strings.TrimSpace(string(versionBytes)))}
 script,err:=a.collectorScript()
 if err!=nil{return err}
 directory:=a.collectorDirectory()
 if err=os.MkdirAll(directory,0700);err!=nil{return err}
 var rooms []string
 if mode=="run"{
  rooms,err=a.collectorRooms()
  if err!=nil{return err}
 }
 port,err:=collectorFreePort()
 if err!=nil{return err}
 c:=a.douyinCollector
 c.mu.Lock()
 defer c.mu.Unlock()
 if c.cmd!=nil{return errors.New("浏览器采集器或登录窗口已在运行，请先停止")}
 stopFile:=filepath.Join(directory,"stop")
 _=os.Remove(stopFile)
 args:=[]string{script,"--profile",filepath.Join(directory,"profile"),
  "--status",filepath.Join(directory,"status.json"),"--stop-file",stopFile,
  "--debug-port",strconv.Itoa(port),"--go-port",strconv.Itoa(a.douyinCollectorPort)}
 if mode=="login"{args=append(args,"--login")}else{args=append(args,"--run","--rooms",strings.Join(rooms,","))}
 cmd:=exec.Command(node,args...)
 cmd.Dir=filepath.Dir(script)
 cmd.Env=append(os.Environ(),"PDJ_DOUYIN_BRIDGE_TOKEN="+a.douyinBridge.token)
 // The child writes a sanitized health report; secrets never enter ordinary logs.
 cmd.Stdout=nil;cmd.Stderr=nil
 if err:=cmd.Start();err!=nil{return fmt.Errorf("启动抖音浏览器失败：%w",err)}
 c.cmd=cmd;c.mode=mode;c.debugPort=port;c.startedAt=time.Now();c.lastError=""
 go func(){
  err:=cmd.Wait()
  c.mu.Lock()
  if c.cmd==cmd {
   c.cmd=nil
   c.stoppedAt=time.Now()
   if err!=nil{c.lastError=err.Error()}
  }
  c.mu.Unlock()
 }()
 return nil
}
func (a *App) stopDouyinCollector() error {
 c:=a.douyinCollector
 c.mu.Lock()
 defer c.mu.Unlock()
 if c.cmd==nil{return nil}
 // Stop via sentinel so Node can close CDP and only its own browser.
 return os.WriteFile(filepath.Join(a.collectorDirectory(),"stop"),[]byte("stop"),0600)
}
func (a *App) shutdownDouyinCollector() { _=a.stopDouyinCollector() }
func (a *App) douyinCollectorState() map[string]any {
 c:=a.douyinCollector
 c.mu.Lock()
 running:=c.cmd!=nil;mode:=c.mode;started:=c.startedAt;lastError:=c.lastError
 c.mu.Unlock()
 payload:=map[string]any{
  "running":running,"mode":mode,"started_at":started,"last_error":lastError,
  "requirements":"Node.js 22+ and Chrome/Edge; isolated browser profile stored locally",
 }
 raw,err:=os.ReadFile(filepath.Join(a.collectorDirectory(),"status.json"))
 if err==nil&&len(raw)<256*1024 {
  var status map[string]any
  if json.Unmarshal(raw,&status)==nil {
   for _,key:=range []string{"login_state","logged_in","checked_at","rooms","updated_at","stop_reason"} {
    if value,ok:=status[key];ok {payload[key]=value}
   }
   if value,ok:=status["last_error"];ok{payload["collector_error"]=value}
  }
 }
 return payload
}
func (a *App) installDouyinCollectorRoutes(mux *http.ServeMux) {
 mux.HandleFunc("GET /api/douyin/room/info",func(w http.ResponseWriter,r *http.Request){
  if !douyinIsLoopback(r)||!a.isAdmin(r){send(w,403,map[string]string{"error":"local admin only"});return}
  instance:=r.URL.Query().Get("instance_id")
  a.mu.RLock()
  var pc ListenerConfig
  found:=false
  for _,item:=range configuredListeners(a.config){
   if item.Platform=="douyin"&&item.ID==instance&&item.Enabled{
    pc=item
    found=true
    break
   }
  }
  a.mu.RUnlock()
  if !found{send(w,404,map[string]string{"error":"enabled Douyin instance not found"});return}
  ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second)
  defer cancel()
  info,err:=(live.Douyin{}).InspectRoom(ctx,pc.Room,pc.Cookie)
  if err!=nil{
   send(w,502,map[string]any{"error":"抖音房间信息读取失败："+err.Error()})
   return
  }
  w.Header().Set("Cache-Control","no-store")
  send(w,200,info)
 })
 mux.HandleFunc("GET /api/douyin/collector/status",func(w http.ResponseWriter,r *http.Request){
  if !douyinIsLoopback(r)||!a.isAdmin(r){send(w,403,map[string]string{"error":"local admin only"});return}
  w.Header().Set("Cache-Control","no-store")
  send(w,200,a.douyinCollectorState())
 })
 mux.HandleFunc("POST /api/douyin/collector/action",func(w http.ResponseWriter,r *http.Request){
  if !douyinIsLoopback(r)||!a.isAdmin(r){send(w,403,map[string]string{"error":"local admin only"});return}
  req:=make(map[string]string)
  if decode(r,&req)!=nil{send(w,400,map[string]string{"error":"invalid JSON"});return}
  var err error
  switch req["action"] {
  case "login":err=a.startDouyinCollector("login")
  case "start":err=a.startDouyinCollector("run")
  case "stop":err=a.stopDouyinCollector()
  default:send(w,400,map[string]string{"error":"action must be login/start/stop"});return
  }
  if err!=nil{send(w,409,map[string]string{"error":err.Error()});return}
  send(w,200,a.douyinCollectorState())
 })
}

