package core

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestDouyinSettingsSaveRoundTripAndDiagnostics(t *testing.T) {
 t.Setenv("BILIPDJ_ADMIN_TOKEN","")
 dir:=t.TempDir()
 a:=New(dir,"0.10.7","test/repo")
 api:=a.Routes(http.NotFoundHandler())
 do:=func(method,path string,body []byte)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,"http://127.0.0.1:9816"+path,bytes.NewReader(body))
  req.RemoteAddr="127.0.0.1:12345"
  req.Header.Set("Origin","http://127.0.0.1:9816")
  recorder:=httptest.NewRecorder()
  api.ServeHTTP(recorder,req)
  return recorder
 }
 cfgResp:=do("GET","/api/config",nil)
 if cfgResp.Code!=200{t.Fatalf("get settings: %d %s",cfgResp.Code,cfgResp.Body.String())}
 var initial struct{Config Config `json:"config"`}
 if e:=json.Unmarshal(cfgResp.Body.Bytes(),&initial);e!=nil{t.Fatal(e)}
 initial.Config.Douyin.Enabled=true
 initial.Config.Douyin.Room="242240793655"
 initial.Config.VisiblePlatforms=[]string{"douyin"}
 body,e:=json.Marshal(initial.Config);if e!=nil{t.Fatal(e)}
 saved:=do("POST","/api/config",body)
 if saved.Code!=200{t.Fatalf("save Douyin config: %d %s",saved.Code,saved.Body.String())}
 confirm:=do("GET","/api/config",nil)
 if confirm.Code!=200 || !bytes.Contains(confirm.Body.Bytes(),[]byte("242240793655")){t.Fatalf("after save: %d %s",confirm.Code,confirm.Body.String())}
 persisted:=New(dir,"0.10.7","test/repo")
 if !persisted.config.Douyin.Enabled||persisted.config.Douyin.Room!="242240793655"{
  t.Fatalf("state.json did not persist Douyin settings: %+v",persisted.config.Douyin)
 }
 location:=do("GET","/api/logs/info",nil)
 if location.Code!=200||!strings.Contains(location.Body.String(),"logs"){t.Fatalf("log path info: %d %s",location.Code,location.Body.String())}
 files,e:=filepath.Glob(filepath.Join(dir,"logs","bilipdj-*.log"))
 if e!=nil||len(files)==0{t.Fatalf("no persistent diagnostics: %v %v",files,e)}
 raw,e:=os.ReadFile(files[0]);if e!=nil{t.Fatal(e)}
 if !bytes.Contains(raw,[]byte("配置已保存")){t.Fatalf("missing saved settings log: %s",raw)}
}

func TestConfigurationValidationFailuresAreLoggedAndDoNotOverwriteState(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","repo")
 cfg:=defaultConfig()
 cfg.Douyin.Enabled=true
 cfg.Douyin.Room="https://not-douyin.example/live"
 payload,_:=json.Marshal(cfg)
 req:=httptest.NewRequest("POST","http://127.0.0.1:9816/api/config",bytes.NewReader(payload))
 req.RemoteAddr="127.0.0.1:40123"
 rr:=httptest.NewRecorder()
 a.Routes(http.NotFoundHandler()).ServeHTTP(rr,req)
 if rr.Code!=400{t.Fatalf("invalid config should fail 400, got %d %s",rr.Code,rr.Body.String())}
 if a.config.Douyin.Enabled{t.Fatal("invalid config replaced current state")}
 files,_:=filepath.Glob(filepath.Join(dir,"logs","bilipdj-*.log"))
 if len(files)==0{t.Fatal("validation failure was not persisted")}
 raw,_:=os.ReadFile(files[0])
 if !bytes.Contains(raw,[]byte("配置保存校验失败")){t.Fatalf("missing failure details: %s",raw)}
}

func TestPersistentLogCredentialRedaction(t *testing.T){
 dir:=t.TempDir()
 d:=newDiagnosticFiles(dir)
 defer d.Close()
 _,err:=d.Write([]byte("login error: sessionid=123secret; SESSDATA=topsecret; PDJ_DOUYIN_BRIDGE_TOKEN=do-not-keep\n"))
 if err!=nil{t.Fatal(err)}
 if err:=d.line("ERROR","douyin","X-PDJ-Bridge-Token=abc access_token=xyz");err!=nil{t.Fatal(err)}
 files,_:=filepath.Glob(filepath.Join(dir,"logs","bilipdj-*.log"))
 if len(files)==0{t.Fatal("log file missing")}
 content,err:=os.ReadFile(files[0]);if err!=nil{t.Fatal(err)}
 for _,secret:=range []string{"123secret","topsecret","do-not-keep","abc","xyz"}{
  if bytes.Contains(content,[]byte(secret)){t.Fatalf("credential was logged: %s",secret)}
 }
 if !bytes.Contains(content,[]byte("[已隐藏]")){t.Fatal("redaction marker missing")}
}
