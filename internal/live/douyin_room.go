package live

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
)

// DouyinRoomDetails contains only public room metadata; it never contains the
// login Cookie/ttwid. Values are best-effort because enter schema may change.
type DouyinRoomDetails struct {
 WebRID string
 RoomID string
 UserUniqueID string
 Title string
 Status int
 ViewerCount string
 AnchorName string
 AnchorID string
}

func (d Douyin) InspectRoom(ctx context.Context, webRID, cookie string) (DouyinRoomDetails,error) {
 info,err:=d.bootstrap(ctx,webRID,cookie)
 if err!=nil{return DouyinRoomDetails{},err}
 result:=DouyinRoomDetails{WebRID:info.LiveID,RoomID:info.RoomID,UserUniqueID:info.UniqueID}
 q:=url.Values{}
 for k,v:=range map[string]string{
  "aid":"6383","app_name":"douyin_web","live_id":"1","device_platform":"web",
  "language":"zh-CN","enter_from":"web_share_link","cookie_enabled":"true",
  "screen_width":"1920","screen_height":"1080","browser_language":"zh-CN",
  "browser_platform":"Win32","browser_name":"Chrome","browser_version":"155.0.0.0",
  "os_name":"Windows","os_version":"10","webcast_sdk_version":"2450",
  "room_id":info.RoomID,"web_rid":info.LiveID,
 }{q.Set(k,v)}
 req,err:=http.NewRequestWithContext(ctx,"GET","https://live.douyin.com/webcast/room/web/enter/?"+q.Encode(),nil)
 if err!=nil{return result,err}
 douyinHeaders(req,cookie)
 req.Header.Set("Referer","https://live.douyin.com/"+url.PathEscape(info.LiveID))
 resp,err:=d.client().Do(req)
 if err!=nil{return result,err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return result,fmt.Errorf("抖音 room/enter HTTP %d",resp.StatusCode)}
 bytes,err:=io.ReadAll(io.LimitReader(resp.Body,2<<20))
 if err!=nil{return result,err}
 // Use a local named type with explicit tags? The wire API uses snake_case;
 // decode a narrow map to avoid a large generated schema.
 var doc map[string]json.RawMessage
 if json.Unmarshal(bytes,&doc)!=nil{return result,errors.New("抖音房间详情 JSON 无法解析")}
 var outer struct{ Data []json.RawMessage }
 if json.Unmarshal(doc["data"],&outer)!=nil || len(outer.Data)==0{
  return result,errors.New("抖音房间详情为空，可能下播或页面协议改变")
 }
 var room struct{
  Status int `json:"status"`
  Title string `json:"title"`
  UserCountStr string `json:"user_count_str"`
  Owner struct{
   Nickname string `json:"nickname"`
   IDStr string `json:"id_str"`
  } `json:"owner"`
 }
 if json.Unmarshal(outer.Data[0],&room)!=nil{return result,errors.New("抖音直播详情格式异常")}
 result.Status=room.Status
 result.Title=room.Title
 result.ViewerCount=room.UserCountStr
 result.AnchorName=room.Owner.Nickname
 result.AnchorID=room.Owner.IDStr
 return result,nil
}

