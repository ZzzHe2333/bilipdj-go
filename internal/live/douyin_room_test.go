package live

import (
 "context"
 "io"
 "net/http"
 "strings"
 "testing"
)

func TestDouyinLiveIDSupportsCopiedShareText(t *testing.T) {
    cases := map[string]string{
        "123456": "123456",
        "https://live.douyin.com/123456?is_from_webapp=1": "123456",
        "分享直播间 复制链接 https://live.douyin.com/123456?enter_from=share 快来": "123456",
    }
    for input, want := range cases {
        got, err := ValidateDouyinRoom(input)
        if err != nil || got != want { t.Errorf("%q -> %q / %v, want %q", input, got, err, want) }
    }
    for _, invalid := range []string{"https://live.douyin.com.evil.example/123", "https://evil.example/123", "javascript:alert(1)", "https://live.douyin.com:9999/123"} {
        if _, err := ValidateDouyinRoom(invalid); err == nil { t.Errorf("unsafe input accepted: %q", invalid) }
    }
}


type inspectDouyinRoundTrip func(*http.Request)(*http.Response,error)
func (fn inspectDouyinRoundTrip) RoundTrip(req *http.Request)(*http.Response,error){return fn(req)}
func TestDouyinRoomDetailsWithoutCookieLeak(t *testing.T) {
 client:=&http.Client{Transport:inspectDouyinRoundTrip(func(req *http.Request)(*http.Response,error){
  var body string
  if req.URL.Path=="/123456789"{
   body = "{\"roomId\":\"7698556487468780322\",\"user_unique_id\":\"10000001\"}"
  }else if req.URL.Path=="/webcast/room/web/enter/"{
   if req.URL.Query().Get("web_rid")!="123456789"{t.Fatal("wrong web RID")}
   body="{\"data\":{\"data\":[{\"status\":2,\"title\":\"我的直播间\",\"user_count_str\":\"2.5万\",\"owner\":{\"nickname\":\"主播昵称\",\"id_str\":\"123\"}}]}}"
  }else{t.Fatal("unexpected path "+req.URL.Path)}
  return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),
   Header:make(http.Header),Request:req},nil
 })}
 out,err:=(Douyin{Client:client}).InspectRoom(context.Background(),"123456789","")
 if err!=nil{t.Fatal(err)}
 if out.WebRID!="123456789"||out.RoomID!="7698556487468780322"||out.Title!="我的直播间"||out.AnchorName!="主播昵称"||out.Status!=2{
  t.Fatalf("bad room metadata: %+v",out)
 }
}
