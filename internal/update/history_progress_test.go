package update

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "runtime"
 "strings"
 "testing"
)

func TestRecentReleaseListingAndExplicitRollback(t *testing.T) {
 name:=fmt.Sprintf("bilipdj-go-%s-%s.zip",runtime.GOOS,runtime.GOARCH)
 archive:=[]byte("download-progress-test-archive")
 sum:=sha256.Sum256(archive)
 hash:=hex.EncodeToString(sum[:])
 var root string
 makeRelease:=func(tag string) map[string]any {
  return map[string]any{"tag_name":tag,"draft":false,"prerelease":false,"assets":[]map[string]any{
   {"name":name,"size":len(archive),"browser_download_url":root+"/zip"},
   {"name":name+".sha256","size":100,"browser_download_url":root+"/sha"},
  }}
 }
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/releases":
   json.NewEncoder(w).Encode([]any{makeRelease("v0.10.4"),makeRelease("v0.10.3"),makeRelease("v0.10.2"),
    map[string]any{"tag_name":"v0.10.1-test","prerelease":true,"assets":[]any{}}})
  case "/releases/latest":
   json.NewEncoder(w).Encode(makeRelease("v0.10.4"))
  case "/sha":
   w.Write([]byte(hash+"  "+name+"\n"))
  case "/zip":
   w.Write(archive)
  default:http.NotFound(w,r)
  }
 }))
 defer server.Close()
 root=server.URL
 service:=New("org/demo","0.10.3",t.TempDir())
 service.APIBase=server.URL
 versions,err:=service.Versions(context.Background())
 if err!=nil||len(versions)!=3{t.Fatalf("version listing: %+v %v",versions,err)}
 if !versions[1].Current||!versions[2].IsOlder||versions[2].Size!=int64(len(archive)){
  t.Fatalf("history metadata: %+v",versions)
 }
 if _,err:=service.DownloadVersion(context.Background(),"v0.10.2",false,nil,"official");err==nil||!strings.Contains(err.Error(),"降级"){
  t.Fatalf("downgrade without consent accepted: %v",err)
 }
 var events []DownloadProgress
 res,err:=service.DownloadVersion(context.Background(),"v0.10.2",true,func(p DownloadProgress){events=append(events,p)},"official")
 if err!=nil||res.Version!="v0.10.2"||res.SHA256!=hash{t.Fatalf("rollback: %+v %v",res,err)}
 foundByteProgress:=false
 for _,p:=range events {
  if p.Phase=="downloading"&&p.DownloadedBytes==int64(len(archive))&&p.TotalBytes==int64(len(archive)){foundByteProgress=true}
 }
 if !foundByteProgress||len(events)==0||events[len(events)-1].Phase!="ready"{
  t.Fatalf("missing accurate progress: %+v",events)
 }
 if _,err:=service.DownloadVersion(context.Background(),"v0.10.1",true,nil,"official");err==nil{
  t.Fatal("unknown or out of window version accepted")
 }
 if _,err:=service.DownloadVersion(context.Background(),"v0.10.3",true,nil,"official");err==nil{
  t.Fatal("re-installing same version accepted")
 }
 if _,err:=service.DownloadVersion(context.Background(),"../v0.10.2",true,nil,"official");err==nil{
  t.Fatal("unsanitized version accepted")
 }
}

func TestHistoryExcludesPreReleaseAndCapsAtTen(t *testing.T){
 s:=New("org/demo","0.10.3",t.TempDir())
 name:=s.filename()
 root:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/releases"{http.NotFound(w,r);return}
  rows:=make([]any,0)
  for i:=15;i>=1;i-- {
   rows=append(rows,map[string]any{"tag_name":fmt.Sprintf("v0.10.%d",i),"assets":[]map[string]any{
    {"name":name,"size":1024},{"name":name+".sha256","size":80}}})
  }
  rows=append([]any{map[string]any{"tag_name":"v99.0.0","prerelease":true,"assets":[]any{}}},rows...)
  json.NewEncoder(w).Encode(rows)
 }))
 defer root.Close()
 s.APIBase=root.URL
 versions,err:=s.Versions(context.Background())
 if err!=nil||len(versions)!=10||versions[0].Version!="v0.10.15"||versions[9].Version!="v0.10.6"{
  t.Fatalf("latest ten formal versions: %+v / %v",versions,err)
 }
}
