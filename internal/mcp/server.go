// Package mcp exposes the running BiliPDJ-Go instance to AI tools.
// It deliberately delegates all queue mutations to the existing HTTP handlers,
// preserving their validation, locking, archives and SSE notifications.
package mcp

import (
 "bytes"
 "crypto/subtle"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/http/httptest"
 "net/url"
 "os"
 "strings"
)

const (
 LegacyVersion = "2025-06-18"
 ModernVersion = "2026-07-28"
 maxRequest = 64 << 10
)

type Server struct {
 Backend http.Handler
 Version string
}

func New(backend http.Handler, version string) *Server { return &Server{Backend: backend, Version: version} }

type rpcRequest struct {
 JSONRPC string          `json:"jsonrpc"`
 ID      json.RawMessage `json:"id,omitempty"`
 Method  string          `json:"method"`
 Params  json.RawMessage `json:"params,omitempty"`
}
type rpcError struct {
 Code int `json:"code"`
 Message string `json:"message"`
}
type rpcReply struct {
 JSONRPC string `json:"jsonrpc"`
 ID json.RawMessage `json:"id"`
 Result any `json:"result,omitempty"`
 Error *rpcError `json:"error,omitempty"`
}
type tool struct {
 Name string `json:"name"`
 Description string `json:"description"`
 InputSchema any `json:"inputSchema"`
 Annotations map[string]any `json:"annotations,omitempty"`
}

func schema(properties map[string]any, required ...string) map[string]any {
 s := map[string]any{"type":"object","properties":properties,"additionalProperties":false}
 if len(required)>0 { s["required"]=required }
 return s
}
func str(desc string) map[string]any { return map[string]any{"type":"string","description":desc} }
func integer(desc string) map[string]any { return map[string]any{"type":"integer","description":desc} }

func availableTools(write bool) []tool {
 read := []tool{
  {"bilipdj_status","读取 B站/抖音监听状态、版本和当前队列人数；不返回 Cookie。",schema(map[string]any{}), map[string]any{"readOnlyHint":true}},
  {"bilipdj_queue","查看当前统一排队列表（用户名、平台、UID、备注、入队时间）。",schema(map[string]any{}), map[string]any{"readOnlyHint":true}},
  {"bilipdj_slots","查看 10 个独立排队存档的数量和当前槽位。",schema(map[string]any{}), map[string]any{"readOnlyHint":true}},
  {"bilipdj_messages","读取最近弹幕消息（不含登录凭据）。",schema(map[string]any{}), map[string]any{"readOnlyHint":true}},
 }
 if !write { return read }
 return append(read,
  tool{"bilipdj_add","在当前存档中手工追加排队成员。",schema(map[string]any{"name":str("用户名，1-60 字"),"note":str("备注，可留空")},"name"), map[string]any{"readOnlyHint":false}},
  tool{"bilipdj_edit","修改当前存档中某条排队的备注，不改变用户身份。",schema(map[string]any{"key":str("来自 bilipdj_queue 的完整队列 key"),"note":str("新备注")},"key","note"), map[string]any{"readOnlyHint":false}},
  tool{"bilipdj_remove","删除当前存档中指定 key 的排队成员。",schema(map[string]any{"key":str("来自 bilipdj_queue 的完整队列 key")},"key"), map[string]any{"readOnlyHint":false,"destructiveHint":true}},
  tool{"bilipdj_move","调整当前存档中指定成员的位置（index 从 0 开始）。",schema(map[string]any{"key":str("完整队列 key"),"index":integer("目标位置，0 起算")},"key","index"), map[string]any{"readOnlyHint":false}},
  tool{"bilipdj_switch_slot","切换所有平台共同使用的当前排队存档槽位。",schema(map[string]any{"slot":integer("1-10 的槽位编号")},"slot"), map[string]any{"readOnlyHint":false}},
  tool{"bilipdj_clear","清空当前槽位排队。必须明确传 confirm=true，操作不可撤销（需从备份恢复）。",schema(map[string]any{"confirm":map[string]any{"type":"boolean","const":true,"description":"必须明确确认清空"}},"confirm"),map[string]any{"readOnlyHint":false,"destructiveHint":true}},
 )
}
func equalToken(got, expected string) bool {
 return expected!="" && len(got)==len(expected) && subtle.ConstantTimeCompare([]byte(got),[]byte(expected))==1
}
func permissions(r *http.Request) (read, write bool) {
 token := ""
 if h:=r.Header.Get("Authorization"); strings.HasPrefix(h,"Bearer ") { token=strings.TrimPrefix(h,"Bearer ") }
 write=equalToken(token,os.Getenv("BILIPDJ_MCP_WRITE_TOKEN"))
 if write || equalToken(token,os.Getenv("BILIPDJ_MCP_READ_TOKEN")) { return true,write }
 host,_,err:=net.SplitHostPort(r.RemoteAddr)
 if err!=nil { return false,false }
 ip:=net.ParseIP(host)
 hostname:=r.Host
 if h,_,e:=net.SplitHostPort(r.Host);e==nil {hostname=h}
 return ip!=nil && ip.IsLoopback() && (hostname=="127.0.0.1"||hostname=="localhost"||hostname=="::1"),false
}
func originAllowed(r *http.Request) bool {
 origin:=r.Header.Get("Origin")
 if origin=="" {return true}
 u,e:=url.Parse(origin)
 return e==nil && (u.Scheme=="http"||u.Scheme=="https") && strings.EqualFold(u.Host,r.Host)
}
func respond(w http.ResponseWriter, code int, v any) {
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.WriteHeader(code)
 _=json.NewEncoder(w).Encode(v)
}
func rpcFail(id json.RawMessage, code int, message string) rpcReply {
 if len(id)==0 { id=json.RawMessage("null") }
 return rpcReply{JSONRPC:"2.0", ID:id,Error:&rpcError{Code:code,Message:message}}
}
func parseParams(raw json.RawMessage, into any) error {
 if len(raw)==0 || string(raw)=="null" {raw=[]byte("{}")}
 dec:=json.NewDecoder(bytes.NewReader(raw))
 dec.DisallowUnknownFields()
 if err:=dec.Decode(into);err!=nil{return err}
 var extra any
 if err:=dec.Decode(&extra);err!=io.EOF {return errors.New("unexpected trailing JSON")}
 return nil
}
func (s *Server) callBackend(method,path string, body any) (any,error) {
 var data []byte
 if body!=nil {data,_=json.Marshal(body)}
 req:=httptest.NewRequest(method,"http://127.0.0.1:9816"+path,bytes.NewReader(data))
 req.RemoteAddr="127.0.0.1:58001"
 if body!=nil {req.Header.Set("Content-Type","application/json")}
 rec:=httptest.NewRecorder()
 s.Backend.ServeHTTP(rec,req)
 if rec.Code>=400 {return nil,fmt.Errorf("BiliPDJ API HTTP %d: %s",rec.Code, strings.TrimSpace(rec.Body.String()))}
 var v any
 if err:=json.Unmarshal(rec.Body.Bytes(),&v);err!=nil {return nil,err}
 return v,nil
}
func (s *Server) call(name string, args json.RawMessage, canWrite bool)(any,error){
 switch name{
 case "bilipdj_status":
  return s.callBackend("GET","/api/status",nil)
 case "bilipdj_queue":
  return s.callBackend("GET","/api/queue/state",nil)
 case "bilipdj_slots":
  return s.callBackend("GET","/api/queue/slots",nil)
 case "bilipdj_messages":
  return s.callBackend("GET","/api/messages",nil)
 }
 if !canWrite {return nil,errors.New("写操作需要 BILIPDJ_MCP_WRITE_TOKEN 授权")}
 switch name{
 case "bilipdj_add":
  var a struct{Name string `json:"name"`; Note string `json:"note"`}
  if e:=parseParams(args,&a);e!=nil{return nil,e}
  if strings.TrimSpace(a.Name)=="" {return nil,errors.New("name required")}
  return s.callBackend("POST","/api/queue",map[string]any{"action":"add","name":a.Name,"note":a.Note})
 case "bilipdj_edit","bilipdj_remove":
  var a struct{Key string `json:"key"`; Note string `json:"note"`}
  if e:=parseParams(args,&a);e!=nil{return nil,e}
  if strings.TrimSpace(a.Key)=="" {return nil,errors.New("key required")}
  action:="edit";if name=="bilipdj_remove" {action="remove"}
  return s.callBackend("POST","/api/queue",map[string]any{"action":action,"key":a.Key,"note":a.Note})
 case "bilipdj_move":
  var a struct{Key string `json:"key"`; Index *int `json:"index"`}
  if e:=parseParams(args,&a);e!=nil{return nil,e}
  if strings.TrimSpace(a.Key)==""||a.Index==nil{return nil,errors.New("key and index required")}
  return s.callBackend("POST","/api/queue",map[string]any{"action":"move","key":a.Key,"index":*a.Index})
 case "bilipdj_switch_slot":
  var a struct{Slot int `json:"slot"`}
  if e:=parseParams(args,&a);e!=nil{return nil,e}
  if a.Slot<1||a.Slot>10{return nil,errors.New("slot must be 1-10")}
  return s.callBackend("POST","/api/queue/slots",map[string]any{"slot":a.Slot})
 case "bilipdj_clear":
  var a struct{Confirm bool `json:"confirm"`}
  if e:=parseParams(args,&a);e!=nil{return nil,e}
  if !a.Confirm{return nil,errors.New("confirm=true required to clear queue")}
  return s.callBackend("POST","/api/queue",map[string]any{"action":"clear"})
 }
 return nil,fmt.Errorf("unknown tool %q",name)
}
func legacyVersion(req rpcRequest, header string) string {
 for _, v := range []string{"2025-03-26","2025-06-18","2025-11-25"} {
  if header==v {return v}
 }
 var p struct{ ProtocolVersion string `json:"protocolVersion"` }
 _=json.Unmarshal(req.Params,&p)
 for _, v := range []string{"2025-03-26","2025-06-18","2025-11-25"} {
  if p.ProtocolVersion==v {return v}
 }
 return LegacyVersion
}
func (s *Server) dispatch(req rpcRequest, canWrite bool, modern bool, requestedVersion string) rpcReply {
 reply:=rpcReply{JSONRPC:"2.0",ID:req.ID}
 result:=map[string]any{}
 if modern {result["resultType"]="complete"}
 switch req.Method {
 case "initialize":
  if modern {return rpcFail(req.ID,-32601,"initialize is not used in 2026 protocol")}
  result=map[string]any{"protocolVersion":requestedVersion,"capabilities":map[string]any{"tools":map[string]any{"listChanged":false}},"serverInfo":map[string]any{"name":"bilipdj-go","version":s.Version}}
 case "server/discover":
  if !modern {return rpcFail(req.ID,-32601,"method not found")}
  result["supportedVersions"]=[]string{ModernVersion}
  result["capabilities"]=map[string]any{"tools":map[string]any{}}
  result["_meta"]=map[string]any{"io.modelcontextprotocol/serverInfo":map[string]any{"name":"bilipdj-go","version":s.Version}}
  result["instructions"]="Read-only by default; queue modifications require an explicit, separate MCP write token."
 case "ping":
  if modern {return rpcFail(req.ID,-32601,"ping is not used in 2026 protocol")}
 case "tools/list":
  result["tools"]=availableTools(canWrite)
 case "tools/call":
  var p struct{Name string `json:"name"`; Arguments json.RawMessage `json:"arguments"`; Meta json.RawMessage `json:"_meta"`}
  if e:=parseParams(req.Params,&p);e!=nil{return rpcFail(req.ID,-32602,"invalid params: "+e.Error())}
  exists:=false
  for _, t:=range availableTools(canWrite) {if t.Name==p.Name {exists=true;break}}
  if !exists {return rpcFail(req.ID,-32602,"unknown or unauthorized tool")}
  value,err:=s.call(p.Name,p.Arguments,canWrite)
  if err!=nil {
   result["isError"]=true
   result["content"]=[]any{map[string]any{"type":"text","text":err.Error()}}
  } else {
   raw,_:=json.Marshal(value)
   result["content"]=[]any{map[string]any{"type":"text","text":string(raw)}}
   if _,ok:=value.(map[string]any);ok {result["structuredContent"]=value}
  }
 default:
  return rpcFail(req.ID,-32601,"method not found")
 }
 reply.Result=result
 return reply
}
func (s *Server) ServeHTTP(w http.ResponseWriter,r *http.Request){
 if r.Method!="POST" {w.Header().Set("Allow","POST");http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return}
 if !originAllowed(r) {respond(w,403,rpcFail(nil,-32001,"origin forbidden"));return}
 read,write:=permissions(r)
 if !read {w.Header().Set("WWW-Authenticate",`Bearer realm="BiliPDJ MCP"`);respond(w,401,rpcFail(nil,-32001,"unauthorized"));return}
 if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")),"application/json") {respond(w,415,rpcFail(nil,-32600,"Content-Type must be application/json"));return}
 raw,e:=io.ReadAll(io.LimitReader(r.Body,maxRequest+1))
 if e!=nil || len(raw)>maxRequest {respond(w,413,rpcFail(nil,-32600,"request too large"));return}
 var request rpcRequest
 if e:=json.Unmarshal(raw,&request);e!=nil {respond(w,400,rpcFail(nil,-32700,"parse error"));return}
 if request.JSONRPC!="2.0" || request.Method=="" {respond(w,400,rpcFail(request.ID,-32600,"invalid JSON-RPC request"));return}
 if len(request.ID)>0 && (string(request.ID)=="null" || (!json.Valid(request.ID))) {respond(w,400,rpcFail(nil,-32600,"invalid ID"));return}
 version:=r.Header.Get("MCP-Protocol-Version")
 modern:=version==ModernVersion
 if version!="" && version!="2025-03-26" && version!=LegacyVersion && version!="2025-11-25" && !modern {respond(w,400,rpcFail(request.ID,-32022,"UnsupportedProtocolVersion"));return}
 if modern {
  var p struct{Meta struct{Version string `json:"io.modelcontextprotocol/protocolVersion"`; Capabilities *json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`} `json:"_meta"`; Name string `json:"name"`}
  if e:=json.Unmarshal(request.Params,&p);e!=nil || p.Meta.Version!=ModernVersion || p.Meta.Capabilities==nil {
   respond(w,400,rpcFail(request.ID,-32600,"missing/mismatched modern MCP _meta"));return
  }
  if r.Header.Get("Mcp-Method")!=request.Method || (request.Method=="tools/call" && r.Header.Get("Mcp-Name")!=p.Name){
   respond(w,400,rpcFail(request.ID,-32020,"HeaderMismatch"));return
  }
 }
 if len(request.ID)==0 {w.WriteHeader(http.StatusAccepted);return}
 respond(w,200,s.dispatch(request,write,modern,legacyVersion(request,version)))
}
