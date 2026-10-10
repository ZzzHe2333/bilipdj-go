package core

import (
 "errors"
 "fmt"
 "regexp"
 "strings"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// ListenerConfig represents another independently authenticated room.
// The historical Config.Bilibili and Config.Douyin fields remain the first
// listener for each service. A listener ID is stable across edits/restarts.
type ListenerConfig struct {
 ID string `json:"id"`
 Platform string `json:"platform"`
 Enabled bool `json:"enabled"`
 Room string `json:"room"`
 Cookie string `json:"cookie,omitempty"`
}

var listenerIDPattern=regexp.MustCompile(`^(bilibili|douyin)-[a-z0-9][a-z0-9-]{2,48}$`)
const maxExtraListeners=30

func configuredListeners(c Config) []ListenerConfig {
 out:=make([]ListenerConfig,0,2+len(c.Listeners))
 out=append(out,ListenerConfig{ID:"bilibili",Platform:"bilibili",Enabled:c.Bilibili.Enabled,Room:c.Bilibili.Room,Cookie:c.Bilibili.Cookie})
 out=append(out,ListenerConfig{ID:"douyin",Platform:"douyin",Enabled:c.Douyin.Enabled,Room:c.Douyin.Room,Cookie:c.Douyin.Cookie})
 out=append(out,c.Listeners...)
 return out
}

func (c Config) listener(id string)(ListenerConfig,bool){
 for _,item:=range configuredListeners(c){if item.ID==id{return item,true}}
 return ListenerConfig{},false
}

// normalize validates every independent instance and prevents an identical
// session from being assigned to two different Bilibili rooms. We never log
// raw Cookie contents. Validate locally; Bilibili may still reject stale
// credentials later at connection time.
func validateRoomListeners(c *Config) error{
 if len(c.Listeners)>maxExtraListeners{return fmt.Errorf("额外直播间最多 %d 个",maxExtraListeners)}
 seenIDs:=map[string]bool{"bilibili":true,"douyin":true}
 for i:=range c.Listeners{
  p:=&c.Listeners[i]
  p.ID=strings.TrimSpace(p.ID)
  p.Platform=strings.TrimSpace(p.Platform)
  p.Room=strings.TrimSpace(p.Room)
  p.Cookie=strings.TrimSpace(p.Cookie)
  if !listenerIDPattern.MatchString(p.ID)||!strings.HasPrefix(p.ID,p.Platform+"-"){
   return errors.New("直播间实例 ID 无效（必须为对应平台加唯一 ID）")
  }
  if seenIDs[p.ID]{return errors.New("直播间实例 ID 重复")}
  seenIDs[p.ID]=true
  if len(p.Cookie)>8192{return errors.New("直播间 Cookie 超过长度限制")}
 }
 rooms:=map[string]string{}
 sessions:=map[string]string{}
 biliEnabled:=0
 for _,p:=range configuredListeners(*c){
  if p.Platform!="bilibili"&&p.Platform!="douyin"{return errors.New("仅 B站与抖音支持新增直播间")}
  if p.Platform=="bilibili"&&p.Enabled{biliEnabled++}
  if p.Room!="" {
   if p.Platform=="bilibili"{
    if _,e:=live.ValidateBilibiliRoom(p.Room);e!=nil{return fmt.Errorf("B站 %s: %w",p.ID,e)}
   }else{
    if _,e:=live.ValidateDouyinRoom(p.Room);e!=nil{return fmt.Errorf("抖音 %s: %w",p.ID,e)}
   }
  }
  if p.Enabled {
   if p.Room==""{return fmt.Errorf("%s 启用前必须填写直播间号",p.ID)}
   roomKey:=p.Platform+":"+p.Room
   if prev,ok:=rooms[roomKey];ok {return fmt.Errorf("同一直播间不可重复监听：%s 和 %s",prev,p.ID)}
   rooms[roomKey]=p.ID
  }
  if p.Platform=="bilibili"&&p.Cookie!="" {
   session:=cookiePart(p.Cookie,"SESSDATA")
   if session!=""&&p.Room!=""{
    if prior,ok:=sessions[session];ok&&prior!=p.ID{return errors.New("B站每个直播间必须使用不同的 Cookie（SESSDATA 重复）")}
    sessions[session]=p.ID
   }
  }
 }
 if biliEnabled>1 {
  for _,p:=range configuredListeners(*c) {
   if p.Platform=="bilibili"&&p.Enabled&&cookiePart(p.Cookie,"SESSDATA")=="" {
    return fmt.Errorf("B站多房间模式中 %s 必须配置独立登录 Cookie（含 SESSDATA）",p.ID)
   }
  }
 }
 return nil
}

func cookiePart(cookie,key string)string{
 for _,s:=range strings.Split(cookie,";"){
  parts:=strings.SplitN(strings.TrimSpace(s),"=",2)
  if len(parts)==2&&strings.EqualFold(strings.TrimSpace(parts[0]),key){return strings.TrimSpace(parts[1])}
 }
 return ""
}

func redactExtraCookies(c Config) Config {
 if len(c.Listeners)>0 {
  c.Listeners=append([]ListenerConfig(nil),c.Listeners...)
  for i:=range c.Listeners{c.Listeners[i].Cookie=""}
 }
 return c
}
func extraCookieConfigured(c Config) map[string]bool{
 m:=make(map[string]bool,len(c.Listeners))
 for _,p:=range c.Listeners{m[p.ID]=p.Cookie!=""}
 return m
}

// assignBiliCookie does not write disk. The caller holds App.mu and must save
// and roll back on error. Disabled listeners are valid QR-login targets too.
func (c *Config) assignBiliCookie(id, cookie string)error{
 if id==""||id=="bilibili" {c.Bilibili.Cookie=cookie;return nil}
 for i:=range c.Listeners{
  if c.Listeners[i].ID==id&&c.Listeners[i].Platform=="bilibili"{
   c.Listeners[i].Cookie=cookie
   return nil
  }
 }
 return errors.New("未找到该 B站直播间配置，请先保存后扫码")
}
