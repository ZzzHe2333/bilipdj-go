package core

import (
    "bytes"
    "compress/gzip"
    "encoding/base64"
    "encoding/binary"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"
)

func dyNum(n int, v uint64) []byte {
    x := binary.AppendUvarint(nil, uint64(n<<3))
    return binary.AppendUvarint(x, v)
}
func dyField(n int, v []byte) []byte {
    x := binary.AppendUvarint(nil, uint64(n<<3|2))
    x = binary.AppendUvarint(x, uint64(len(v)))
    return append(x,v...)
}
func dyFrame(msgID uint64, count int, end bool, room uint64) string {
    common := append(dyNum(2,msgID),dyNum(3,room)...)
    common = append(common, dyNum(4,1792464867000)...)
    user := append(dyNum(1,787702714468572), dyField(3,[]byte("tester"))...)
    gift := append(dyNum(5,3992), dyNum(12,1)...)
    gift = append(gift, dyField(16,[]byte("人气票"))...)
    body := append(dyField(1,common), dyNum(2,3992)...)
    body = append(body, dyNum(5,uint64(count))...)
    body = append(body, dyField(7,user)...)
    if end { body=append(body, dyNum(9,1)...) }
    body=append(body,dyField(15,gift)...)
    msg := append(dyField(1,[]byte("WebcastGiftMessage")),dyField(2,body)...)
    response:=dyField(1,msg)
    var compressed bytes.Buffer
    gz:=gzip.NewWriter(&compressed)
    _,_=gz.Write(response)
    _=gz.Close()
    return base64.StdEncoding.EncodeToString(dyField(8,compressed.Bytes()))
}
func dyPOST(a *App,token string,frame string,room string,remote string) *httptest.ResponseRecorder {
    requestBody,_:=json.Marshal(map[string]string{"instance_id":"douyin","live_id":room,"frame":frame})
    req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:9816/api/douyin/bridge/frame",bytes.NewReader(requestBody))
    req.RemoteAddr=remote
    req.Header.Set("Content-Type","application/json")
    req.Header.Set("X-PDJ-Bridge-Token",token)
    rr:=httptest.NewRecorder()
    a.Routes(http.NotFoundHandler()).ServeHTTP(rr,req)
    return rr
}
func TestDouyinBridgeRequiresPairingAndRoom(t *testing.T) {
    a:=New(t.TempDir(),"test","ZzzHe2333/bilipdj-go")
    a.config.Douyin.Room="980468525827"
    a.config.Douyin.Enabled=true
    frame:=dyFrame(1,1,true,7698556487468780322)
    for _,tc:=range []struct{token,room,remote string; status int}{
        {"invalid","980468525827","127.0.0.1:1234",401},
        {a.douyinBridge.token,"980468525827","192.168.1.2:1234",403},
        {a.douyinBridge.token,"999999999","127.0.0.1:1234",409},
    } {
        resp:=dyPOST(a,tc.token,frame,tc.room,tc.remote)
        if resp.Code!=tc.status {t.Fatalf("status %d, wanted %d: %s",resp.Code,tc.status,resp.Body)}
    }
}
func TestDouyinBridgeComboAndNoPaymentCredit(t *testing.T) {
    a:=New(t.TempDir(),"test","ZzzHe2333/bilipdj-go")
    a.config.Douyin.Room="980468525827"
    a.config.Douyin.Enabled=true
    a.config.GiftQueue.Enabled=true
    a.config.GiftQueue.Names=[]string{"人气票"}
    for _,x:=range []struct{id uint64; n int; end bool}{
        {1,1,false},{2,57,false},{3,131,false},{4,520,true},{4,520,true},
    } {
        resp:=dyPOST(a,a.douyinBridge.token,dyFrame(x.id,x.n,x.end,7698556487468780322),"980468525827","127.0.0.1:1234")
        if resp.Code!=200 {t.Fatalf("status=%d %s",resp.Code,resp.Body)}
    }
    if a.giftLast==nil || a.giftLast.Gift.Count!=520 || a.giftLast.Gift.Diamonds!=520 {
        t.Fatalf("combo not finalized once: %+v",a.giftLast)
    }
    if len(a.giftCredits)!=0 { t.Fatal("browser frame was granted paid queue credits") }
    if len(a.douyinBridge.pending)!=0 { t.Fatal("pending combo not removed") }
    // A late, older cumulative update must not start a second combo.
    resp:=dyPOST(a,a.douyinBridge.token,dyFrame(5,100,false,7698556487468780322),"980468525827","127.0.0.1:1234")
    if resp.Code!=200 || len(a.douyinBridge.pending)!=0 {t.Fatalf("stale event was reopened: %v",resp.Code)}
}
func TestDouyinComboTimeout(t *testing.T) {
    a:=New(t.TempDir(),"test","ZzzHe2333/bilipdj-go")
    a.config.Douyin.Room="980468525827"
    a.config.Douyin.Enabled=true
    resp:=dyPOST(a,a.douyinBridge.token,dyFrame(100,2,false,7698556487468780322),"980468525827","127.0.0.1:1234")
    if resp.Code!=200 {t.Fatal(resp.Code)}
    // Simulate idle for deterministic testing rather than sleeping four seconds.
    b:=a.douyinBridge
    b.mu.Lock()
    for _,p:=range b.pending {p.timer.Stop();p.lastSeen=time.Now().Add(-5*time.Second)}
    var key string
    for k:=range b.pending {key=k}
    b.mu.Unlock()
    a.flushDouyinCombo(key,"douyin","980468525827")
    if a.giftLast==nil || a.giftLast.Gift.Count!=2 {t.Fatal("idle combo not finalized")}
}
