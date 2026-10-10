package update

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "runtime"
 "strings"
 "testing"
)

func TestCommunityProxySourceSelectionAndIntegrity(t *testing.T) {
 name := fmt.Sprintf("bilipdj-go-%s-%s.zip",runtime.GOOS,runtime.GOARCH)
 archive := []byte("BiliPDJ updater test archive")
 digest := sha256.Sum256(archive)
 sha := hex.EncodeToString(digest[:])
 manifest := map[string]any{
  "version":"0.10.3","tag_name":"v0.10.3",
  "packages":map[string]any{name:map[string]any{"name":name,"size":len(archive),"sha256":sha}},
 }
 payload,_ := json.Marshal(manifest)
 for _,proxy := range communityProxies {
  t.Run(proxy.name,func(t *testing.T){
   used := []string{}
   client := &http.Client{Transport:roundTripFunc(func(r *http.Request)(*http.Response,error){
    used=append(used,r.URL.String())
    var data []byte
    switch{
    case !strings.HasPrefix(r.URL.String(),proxy.prefix+"https://github.com/"):
     return &http.Response{StatusCode:503,Body:io.NopCloser(strings.NewReader("wrong host")),Header:http.Header{}},nil
    case strings.HasSuffix(r.URL.Path,"update-manifest.json"):
     data=payload
    case strings.HasSuffix(r.URL.Path,name):
     data=archive
    default:
     return &http.Response{StatusCode:404,Body:io.NopCloser(strings.NewReader("not found")),Header:http.Header{}},nil
    }
    return &http.Response{StatusCode:200,Body:io.NopCloser(bytes.NewReader(data)),Header:http.Header{}},nil
   })}
   service := New("ZzzHe2333/bilipdj-go","0.10.2",t.TempDir())
   service.Client=client
   checked,err:=service.Check(context.Background(),proxy.name)
   if err!=nil||checked.Source!=proxy.name||!checked.UpdateAvailable {
    t.Fatalf("check proxy %q: %+v %v",proxy.name,checked,err)
   }
   downloaded,err:=service.Download(context.Background(),proxy.name)
   if err!=nil||downloaded.SHA256!=sha||downloaded.Version!="v0.10.3"{
    t.Fatalf("download proxy %q: %+v %v",proxy.name,downloaded,err)
   }
   if len(used)!=3{t.Fatalf("should use one proxy for check+manifest+zip, got %v",used)}
   for _,u:=range used {
    if !strings.HasPrefix(u,proxy.prefix+"https://github.com/ZzzHe2333/bilipdj-go/releases/"){
     t.Fatalf("unexpected proxy target: %s",u)
    }
   }
  })
 }
}

func TestAllCommunitySourcesHaveFixedURLs(t *testing.T){
 target:="https://github.com/ZzzHe2333/bilipdj-go/releases/download/v0.10.3/test.zip"
 all:=sourceCandidates(target,"auto",false)
 if len(all)!=len(communityProxies)+1||all[0]!=target{
  t.Fatalf("auto source precedence %v",all)
 }
 pool:=sourceCandidates(target,"accelerated",false)
 if len(pool)!=len(communityProxies)+1||pool[len(pool)-1]!=target {
  t.Fatalf("accelerated fallback precedence %v",pool)
 }
 for i,p:=range communityProxies {
  if !validUpdateSource(p.name)||all[i+1]!=p.prefix+target||pool[i]!=p.prefix+target {
   t.Errorf("proxy route mismatch %q",p.name)
  }
  chosen:=sourceCandidates(target,p.name,false)
  if len(chosen)!=1||chosen[0]!=p.prefix+target {t.Errorf("selected source leaked fallback %q: %v",p.name,chosen)}
 }
 if len(sourceCandidates(target,"official",false))!=1||sourceCandidates(target,"official",false)[0]!=target {
  t.Fatal("official source rewritten to proxy")
 }
 if len(sourceCandidates(target,"akams",true))!=1||sourceCandidates(target,"akams",true)[0]!=target {
  t.Fatal("test API override must bypass proxies")
 }
 for _,bad:=range []string{"","https://example.com/","evil","http://gh.dpik.top/"}{
  if validUpdateSource(bad){t.Errorf("invalid source accepted: %q",bad)}
 }
}

func TestCommunityFallbackSkipsInvalidManifest(t *testing.T){
 archive:=[]byte("valid")
 sum:=sha256.Sum256(archive)
 name:=fmt.Sprintf("bilipdj-go-%s-%s.zip",runtime.GOOS,runtime.GOARCH)
 manifest,_:=json.Marshal(map[string]any{
  "version":"0.10.3","tag_name":"v0.10.3",
  "packages":map[string]any{name:map[string]any{"name":name,"size":len(archive),"sha256":hex.EncodeToString(sum[:])}},
 })
 calls:=[]string{}
 service:=New("ZzzHe2333/bilipdj-go","0.10.2",t.TempDir())
 service.Client=&http.Client{Transport:roundTripFunc(func(r *http.Request)(*http.Response,error){
  calls=append(calls,r.URL.String())
  var content []byte
  code:=503
  if strings.HasPrefix(r.URL.String(),ProxyPrefix) {
   code=200;content=[]byte(`{"version":"evil","tag_name":"wrong"}`)
  }else if strings.HasPrefix(r.URL.String(),ProxyAlternate) {
   code=200;content=[]byte("<html>proxy unavailable</html>")
  }else if strings.HasPrefix(r.URL.String(),ProxyAkams) {
   code=200;content=manifest
  }else if strings.HasPrefix(r.URL.String(),ProxyGHFile) {
   t.Fatalf("should stop after first valid proxy")
  }
  return &http.Response{StatusCode:code,Header:http.Header{},Body:io.NopCloser(bytes.NewReader(content))},nil
 })}
 got,err:=service.Check(context.Background(),"accelerated")
 if err!=nil||!got.UpdateAvailable||got.Source!="accelerated" {t.Fatalf("fallback %+v err %v, calls=%v",got,err,calls)}
 if len(calls)!=3||!strings.HasPrefix(calls[2],ProxyAkams) {t.Fatalf("failed fallback order %v",calls)}
}

func TestCommunityZIPTamperFailsIntegrityCheck(t *testing.T){
 name:=fmt.Sprintf("bilipdj-go-%s-%s.zip",runtime.GOOS,runtime.GOARCH)
 good:=[]byte("original")
 sum:=sha256.Sum256(good)
 manifest,_:=json.Marshal(map[string]any{
  "version":"0.10.3","tag_name":"v0.10.3",
  "packages":map[string]any{name:map[string]any{"name":name,"size":len(good),"sha256":hex.EncodeToString(sum[:])}},
 })
 service:=New("ZzzHe2333/bilipdj-go","0.10.2",t.TempDir())
 service.Client=&http.Client{Transport:roundTripFunc(func(r *http.Request)(*http.Response,error){
  var data []byte
  if strings.HasSuffix(r.URL.Path,"update-manifest.json"){data=manifest}else{data=[]byte("tampered")}
  return &http.Response{StatusCode:200,Header:http.Header{},Body:io.NopCloser(bytes.NewReader(data))},nil
 })}
 _,err:=service.Download(context.Background(),"gh-dpik")
 if err==nil||!strings.Contains(err.Error(),"SHA256") {
  t.Fatalf("tampered ZIP accepted: %v",err)
 }
}
