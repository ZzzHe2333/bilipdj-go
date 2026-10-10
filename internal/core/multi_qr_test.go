package core

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestQRLoginBindsOnlyRequestedBilibiliRoom(t *testing.T){
 upstream:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/generate":
   _,_ = w.Write([]byte(`{"code":0,"data":{"url":"https://passport.bilibili.com/qrcode/secure","qrcode_key":"private-key"}}`))
  case "/poll":
   w.Header().Add("Set-Cookie","SESSDATA=room-two-only; Path=/; HttpOnly")
   _,_ = w.Write([]byte(`{"code":0,"data":{"code":0,"url":"https://www.bilibili.com/?DedeUserID=901&bili_jct=abc"}}`))
  case "/nav":
   if !strings.Contains(r.Header.Get("Cookie"),"SESSDATA=room-two-only"){
    t.Error("QR nav did not use authenticated room session")
   }
   _,_ = w.Write([]byte(`{"code":0,"data":{"isLogin":true,"mid":901,"uname":"room-user"}}`))
  default:
   t.Errorf("unexpected Bilibili test endpoint %q",r.URL.Path)
  }
 }))
 defer upstream.Close()
 a:=New(t.TempDir(),"0.10.4","org/repo")
 a.config.Bilibili.Cookie="SESSDATA=original"
 a.config.Bilibili.Room="123"
 a.config.Listeners=[]ListenerConfig{extraRoom("bilibili-room-two","bilibili","456","",false)}
 a.qrGenerateURL=upstream.URL+"/generate"
 a.qrPollURL=upstream.URL+"/poll"
 a.qrNavURL=upstream.URL+"/nav"
 server:=httptest.NewServer(a.Routes(http.NotFoundHandler()))
 defer server.Close()
 post:=func(route,body string)(int,string){
  t.Helper()
  resp,err:=http.Post(server.URL+route,"application/json",strings.NewReader(body))
  if err!=nil{t.Fatal(err)}
  defer resp.Body.Close()
  var v map[string]any
  if err=json.NewDecoder(resp.Body).Decode(&v);err!=nil{t.Fatal(err)}
  raw,_:=json.Marshal(v)
  return resp.StatusCode,string(raw)
 }
 if code,_:=post("/api/bili/qr/start",`{"instance_id":"bilibili-room-missing"}`);code!=400{t.Fatalf("unknown room start status %d",code)}
 code,_:=post("/api/bili/qr/start",`{"instance_id":"bilibili-room-two"}`)
 if code!=200{t.Fatalf("targeted QR start status %d",code)}
 code,result:=post("/api/bili/qr/poll",`{"instance_id":"bilibili-room-two"}`)
 if code!=200||!strings.Contains(result,"success")||strings.Contains(result,"room-two-only"){t.Fatalf("targeted QR finish: %d %s",code,result)}
 if a.config.Bilibili.Cookie!="SESSDATA=original"{t.Fatal("targeted QR overwrote primary login Cookie")}
 if !strings.Contains(a.config.Listeners[0].Cookie,"SESSDATA=room-two-only"){t.Fatal("targeted QR did not save extra room Cookie")}
 configResp,err:=http.Get(server.URL+"/api/config")
 if err!=nil{t.Fatal(err)}
 defer configResp.Body.Close()
 var v map[string]any
 if err=json.NewDecoder(configResp.Body).Decode(&v);err!=nil{t.Fatal(err)}
 raw,_:=json.Marshal(v)
 if strings.Contains(string(raw),"room-two-only")||strings.Contains(string(raw),"original"){t.Fatal("config API leaked Cookie")}
 code,_=post("/api/bili/logout",`{"instance_id":"bilibili-room-two"}`)
 if code!=200{t.Fatalf("room logout: %d",code)}
 if a.config.Bilibili.Cookie!="SESSDATA=original"||a.config.Listeners[0].Cookie!=""{t.Fatal("logout affected wrong Cookie")}
}
