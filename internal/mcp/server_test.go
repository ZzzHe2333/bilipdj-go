package mcp

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/ZzzHe2333/bilipdj-go/internal/core"
 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func testServer(t *testing.T) (*Server,*core.App) {
 t.Helper()
 a:=core.New(t.TempDir(),"0.10.1","ZzzHe2333/bilipdj-go")
 return New(a.Routes(http.NotFoundHandler()),"0.10.1"),a
}
func rpcHTTP(t *testing.T,s *Server,method string,args any, id int,token,remote string,modern bool) (int,map[string]any) {
 t.Helper()
 params:=map[string]any{}
 if args!=nil {params=args.(map[string]any)}
 if modern {
  params["_meta"]=map[string]any{"io.modelcontextprotocol/protocolVersion":ModernVersion,"io.modelcontextprotocol/clientCapabilities":map[string]any{},"io.modelcontextprotocol/clientInfo":map[string]any{"name":"mcp-test","version":"1"}}
 }
 payload,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":id,"method":method,"params":params})
 req:=httptest.NewRequest("POST","http://127.0.0.1:9816/mcp",bytes.NewReader(payload))
 req.RemoteAddr=remote
 req.Header.Set("Content-Type","application/json")
 if token!="" {req.Header.Set("Authorization","Bearer "+token)}
 if modern {
  req.Header.Set("MCP-Protocol-Version",ModernVersion)
  req.Header.Set("Mcp-Method",method)
  if method=="tools/call" {req.Header.Set("Mcp-Name",params["name"].(string))}
 }
 rr:=httptest.NewRecorder()
 s.ServeHTTP(rr,req)
 var parsed map[string]any
 _=json.Unmarshal(rr.Body.Bytes(),&parsed)
 return rr.Code,parsed
}
func toolRPC(t *testing.T,s *Server,name string,args map[string]any,token string,modern bool) (int,map[string]any) {
 t.Helper()
 return rpcHTTP(t,s,"tools/call",map[string]any{"name":name,"arguments":args},6,token,"127.0.0.1:5100",modern)
}
func resultText(t *testing.T,v map[string]any) string {
 t.Helper()
 res,ok:=v["result"].(map[string]any)
 if !ok {t.Fatalf("missing result: %v",v)}
 content,ok:=res["content"].([]any)
 if !ok||len(content)==0 {t.Fatalf("missing MCP content: %v",v)}
 return content[0].(map[string]any)["text"].(string)
}
func TestLegacyAndModernReadOnly(t *testing.T){
 t.Setenv("BILIPDJ_MCP_WRITE_TOKEN","")
 t.Setenv("BILIPDJ_MCP_READ_TOKEN","")
 s,a:=testServer(t)
 a.OnDanmu(live.Event{Platform:"bilibili",UserID:"10",Username:"A",Content:"排队",Time:time.Now()})
 a.OnDanmu(live.Event{Platform:"douyin",UserID:"10",Username:"B",Content:"排队",Time:time.Now()})
 code,r:=rpcHTTP(t,s,"initialize",map[string]any{},1,"","127.0.0.1:3",false)
 if code!=200||r["error"]!=nil {t.Fatalf("init %d %v",code,r)}
 init:=r["result"].(map[string]any)
 if init["protocolVersion"]!=LegacyVersion {t.Fatal(init)}
 code,r=rpcHTTP(t,s,"tools/list",map[string]any{},2,"","127.0.0.1:3",false)
 if code!=200 {t.Fatal(code,r)}
 list:=r["result"].(map[string]any)["tools"].([]any)
 if len(list)!=4 {t.Fatalf("read-only tools expected 4 got %d",len(list))}
 code,r=toolRPC(t,s,"bilipdj_queue",map[string]any{},"",false)
 if code!=200||!strings.Contains(resultText(t,r),"douyin")||!strings.Contains(resultText(t,r),"bilibili"){t.Fatal(r)}
 code,r=rpcHTTP(t,s,"server/discover",map[string]any{},3,"","127.0.0.1:3",true)
 if code!=200||r["result"].(map[string]any)["resultType"]!="complete" {t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_slots",map[string]any{},"",true)
 if code!=200||r["result"].(map[string]any)["resultType"]!="complete"{t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_clear",map[string]any{"confirm":true},"",false)
 if code!=200||r["error"]==nil {t.Fatal("unauthorized write exposed",code,r)}
}
func TestWriteTokenAndQueueMutations(t *testing.T){
 t.Setenv("BILIPDJ_MCP_WRITE_TOKEN","test-MCP-strong-token")
 t.Setenv("BILIPDJ_MCP_READ_TOKEN","readonly-secret")
 s,_:=testServer(t)
 code,r:=rpcHTTP(t,s,"tools/list",map[string]any{},1,"test-MCP-strong-token","203.0.113.11:4000",false)
 if code!=200||len(r["result"].(map[string]any)["tools"].([]any))!=10 {t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_add",map[string]any{"name":"MCP Agent","note":"测试"},"test-MCP-strong-token",false)
 if code!=200||r["error"]!=nil {t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_queue",map[string]any{},"",false)
 if !strings.Contains(resultText(t,r),"MCP Agent") {t.Fatal(r)}
 var q struct {Entries []struct{Key string `json:"key"`} `json:"entries"`}
 if e:=json.Unmarshal([]byte(resultText(t,r)),&q);e!=nil||len(q.Entries)!=1 {t.Fatal(e,r)}
 key:=q.Entries[0].Key
 code,r=toolRPC(t,s,"bilipdj_edit",map[string]any{"key":key,"note":"修改成功"},"test-MCP-strong-token",true)
 if code!=200||!strings.Contains(resultText(t,r),"修改成功"){t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_clear",map[string]any{"confirm":false},"test-MCP-strong-token",false)
 if code!=200||r["result"].(map[string]any)["isError"]!=true {t.Fatal("clear must require confirm",code,r)}
 code,r=toolRPC(t,s,"bilipdj_switch_slot",map[string]any{"slot":2},"test-MCP-strong-token",false)
 if code!=200||!strings.Contains(resultText(t,r),`"active_slot":2`) {t.Fatal(code,r)}
 code,r=toolRPC(t,s,"bilipdj_switch_slot",map[string]any{"slot":1},"test-MCP-strong-token",false)
 if code!=200||!strings.Contains(resultText(t,r),"MCP Agent"){t.Fatal("slot lost",code,r)}
 code,r=toolRPC(t,s,"bilipdj_remove",map[string]any{"key":key},"test-MCP-strong-token",false)
 if code!=200||strings.Contains(resultText(t,r),"MCP Agent") {t.Fatal(code,r)}
}
func TestRemoteAuthOriginAndModernHeaders(t *testing.T){
 t.Setenv("BILIPDJ_MCP_READ_TOKEN","read-secret")
 t.Setenv("BILIPDJ_MCP_WRITE_TOKEN","write-secret")
 s,_:=testServer(t)
 code,_:=rpcHTTP(t,s,"tools/list",map[string]any{},1,"","198.51.100.4:1111",false)
 if code!=401 {t.Fatal("remote without bearer must fail",code)}
 code,r:=rpcHTTP(t,s,"tools/list",map[string]any{},1,"read-secret","198.51.100.4:1111",false)
 if code!=200||len(r["result"].(map[string]any)["tools"].([]any))!=4 {t.Fatal("remote read",code,r)}
 code,r=rpcHTTP(t,s,"tools/list",map[string]any{},1,"write-secret","198.51.100.4:1111",false)
 if code!=200||len(r["result"].(map[string]any)["tools"].([]any))!=10 {t.Fatal("remote write",code,r)}
 req:=httptest.NewRequest("POST","http://127.0.0.1:9816/mcp",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Origin","https://attacker.example")
 req.RemoteAddr="127.0.0.1:55"
 rr:=httptest.NewRecorder();s.ServeHTTP(rr,req)
 if rr.Code!=403 {t.Fatal("cross-origin allowed",rr.Code)}
 req=httptest.NewRequest("POST","http://127.0.0.1:9816/mcp",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`))
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("MCP-Protocol-Version",ModernVersion)
 req.RemoteAddr="127.0.0.1:55"
 rr=httptest.NewRecorder();s.ServeHTTP(rr,req)
 if rr.Code!=400 {t.Fatal("missing mirrored method accepted",rr.Code)}
}
func TestStdioProxyNoSecondBackend(t *testing.T){
 t.Setenv("BILIPDJ_MCP_READ_TOKEN","")
 t.Setenv("BILIPDJ_MCP_WRITE_TOKEN","")
 s,_:=testServer(t)
 ts:=httptest.NewServer(s);defer ts.Close()
 t.Setenv("BILIPDJ_MCP_URL",ts.URL+"/mcp")
 input:=strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\",\"params\":{}}\n")
 var out bytes.Buffer
 if e:=RunStdio(input,&out);e!=nil {t.Fatal(e)}
 lines:=strings.Split(strings.TrimSpace(out.String()),"\n")
 if len(lines)!=2 {t.Fatal(out.String())}
 for _,line:=range lines {
  var value map[string]any
  if e:=json.Unmarshal([]byte(line),&value);e!=nil||value["error"]!=nil {t.Fatal(line,e)}
 }
}
func TestReadOnlyNeverReturnsPlatformCookies(t *testing.T){
 t.Setenv("BILIPDJ_MCP_WRITE_TOKEN","")
 s,_:=testServer(t)
 code,r:=toolRPC(t,s,"bilipdj_status",map[string]any{},"",false)
 if code!=200||strings.Contains(resultText(t,r),"SESSDATA"){t.Fatal(code,r)}
}
var _ = os.Stderr
