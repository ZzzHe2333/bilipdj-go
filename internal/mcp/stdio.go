package mcp

import (
 "bufio"
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "os"
 "time"
)

// RunStdio relays newline-delimited MCP JSON-RPC messages to an already
// running BiliPDJ-Go process. It never opens its own state files or live feeds.
// No non-protocol output is ever written to stdout.
// MCP tool responses can be much larger than tool arguments, especially
// when the queue or recent chat contains many messages.
const maxStdioResponse = 4 << 20

func RunStdio(in io.Reader, out io.Writer) error {
 endpoint:=os.Getenv("BILIPDJ_MCP_URL")
 if endpoint=="" {endpoint="http://127.0.0.1:9816/mcp"}
 u,err:=url.Parse(endpoint)
 if err!=nil || u.Host=="" || u.Path!="/mcp" || (u.Scheme!="https"&&u.Scheme!="http") {return errors.New("BILIPDJ_MCP_URL must be an HTTP(S) /mcp endpoint")}
 if u.Scheme=="http" {
  h:=u.Hostname()
  ip:=net.ParseIP(h)
  if h!="localhost" && (ip==nil || !ip.IsLoopback()) {return errors.New("remote MCP servers require HTTPS")}
 }
 // Do not silently inherit the server's write credential. An AI client
 // must explicitly opt in to privileged tools with its own client token.
 token:=os.Getenv("BILIPDJ_MCP_CLIENT_TOKEN")
 if token=="" {token=os.Getenv("BILIPDJ_MCP_READ_TOKEN")}
 client:=&http.Client{Timeout:30*time.Second,CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}}
 scanner:=bufio.NewScanner(in)
 scanner.Buffer(make([]byte,4096),maxRequest+1)
 writer:=bufio.NewWriter(out)
 for scanner.Scan(){
  data:=bytes.TrimSpace(scanner.Bytes())
  if len(data)==0 {continue}
  var req rpcRequest
  if json.Unmarshal(data,&req)!=nil || req.JSONRPC!="2.0" || req.Method=="" {
   b,_:=json.Marshal(rpcFail(nil,-32700,"invalid JSON-RPC"))
   fmt.Fprintln(writer,string(b)); if e:=writer.Flush();e!=nil{return e};continue
  }
  var params struct {
   Name string `json:"name"`
   Meta struct{Version string `json:"io.modelcontextprotocol/protocolVersion"`} `json:"_meta"`
  }
  _=json.Unmarshal(req.Params,&params)
  ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
  httpReq,e:=http.NewRequestWithContext(ctx,"POST",endpoint,bytes.NewReader(data))
  if e==nil {
   httpReq.Header.Set("Content-Type","application/json")
   httpReq.Header.Set("Accept","application/json, text/event-stream")
   if token!="" {httpReq.Header.Set("Authorization","Bearer "+token)}
   if params.Meta.Version==ModernVersion {
    httpReq.Header.Set("MCP-Protocol-Version",ModernVersion)
    httpReq.Header.Set("Mcp-Method",req.Method)
    if req.Method=="tools/call" {httpReq.Header.Set("Mcp-Name",params.Name)}
   }
   var response *http.Response
   response,e=client.Do(httpReq)
   if e==nil {
    var b []byte
    b,e=io.ReadAll(io.LimitReader(response.Body,maxStdioResponse+1))
    response.Body.Close()
    if e==nil && response.StatusCode==http.StatusAccepted {cancel();continue}
    if e==nil && len(b)<=maxStdioResponse && json.Valid(b) {
     if len(req.ID)>0 {
      if _,e=writer.Write(bytes.TrimSpace(b));e==nil {e=writer.WriteByte('\n')}
      if e==nil {e=writer.Flush()}
     }
     cancel()
     if e!=nil {return e}
     continue
    }
    if e==nil {e=fmt.Errorf("HTTP %d invalid MCP response",response.StatusCode)}
   }
  }
  cancel()
  if len(req.ID)==0 {continue}
  b,_:=json.Marshal(rpcFail(req.ID,-32000,"MCP HTTP bridge unavailable: "+e.Error()))
  if _,e=writer.Write(b);e!=nil{return e}
  if e=writer.WriteByte('\n');e!=nil{return e}
  if e=writer.Flush();e!=nil{return e}
 }
 return scanner.Err()
}
