package core

import (
 "errors"
 "fmt"
 "strings"
 "unicode/utf8"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// PermissionEntry uses a nickname as requested. A platform binds the name to
// Bilibili, Douyin, or both. Future versions can add verified platform user IDs.
type PermissionEntry struct {
 Name string `json:"id"`
 Platform string `json:"platform"`
 Role string `json:"role"`
 Capabilities []string `json:"capabilities,omitempty"`
}
var permissionCaps=[]string{"moderate","queue","insert","switch","limits"}
var adminDefaults=[]string{"moderate","queue","insert","switch","limits"}
var parttimeDefaults=[]string{"queue"}

func copyCaps(v []string)[]string{return append([]string(nil),v...)}
func containsCap(v []string,needle string)bool{for _,c:=range v{if c==needle{return true}};return false}

func cleanPermissions(entries []PermissionEntry)([]PermissionEntry,error){
 if len(entries)>1500{return nil,errors.New("身份名单超过 1500 条")}
 if entries==nil{return nil,nil}
 out:=make([]PermissionEntry,0,len(entries))
 seen:=map[string]bool{}
 for _,p:=range entries{
  p.Name=strings.TrimSpace(p.Name)
  p.Platform=strings.TrimSpace(p.Platform)
  if p.Name==""||utf8.RuneCountInString(p.Name)>60||strings.ContainsAny(p.Name,"\r\n\x00"){return nil,errors.New("昵称必须为 1–60 字，不能包含换行")}
  switch p.Platform{case "all","bilibili","douyin":default:return nil,fmt.Errorf("不支持的权限平台 %q",p.Platform)}
  switch p.Role{case "super_admin","admin","part_time","user","blacklist":default:return nil,fmt.Errorf("无效身份 %q",p.Role)}
  key:=p.Platform+"\x00"+strings.ToLower(p.Name)
  if seen[key]{return nil,errors.New("同一平台不可重复配置该昵称")}
  seen[key]=true
  if p.Role=="admin"||p.Role=="part_time"{
   used:=map[string]bool{}
   caps:=[]string{}
   for _,cap:=range p.Capabilities{
    if !containsCap(permissionCaps,cap){return nil,fmt.Errorf("未知管理员权限 %q",cap)}
    if !used[cap]{caps=append(caps,cap);used[cap]=true}
   }
   p.Capabilities=caps
  }else{p.Capabilities=nil}
  out=append(out,p)
 }
 return out,nil
}
func legacyPermissions(c Config)[]PermissionEntry{
 out:=[]PermissionEntry{}
 add:=func(names []string,role string){
  for _,name:=range names{
   name=strings.TrimSpace(name)
   if name==""{continue}
   found:=false
   for _,old:=range out{if strings.EqualFold(old.Name,name){found=true;break}}
   if !found{
    row:=PermissionEntry{Name:name,Platform:"all",Role:role}
    if role=="admin"{row.Capabilities=copyCaps(adminDefaults)}
    if role=="part_time"{row.Capabilities=copyCaps(parttimeDefaults)}
    out=append(out,row)
   }
  }
 }
 add(c.Blacklist,"blacklist")
 add(c.SuperAdmins,"super_admin")
 add(c.Admins,"admin")
 add(c.Guards,"part_time")
 return out
}
func (c Config) permissionRows()[]PermissionEntry{
 if c.Permissions!=nil{return append([]PermissionEntry{},c.Permissions...)}
 return legacyPermissions(c)
}
func (c Config) effectiveEntry(e live.Event)(PermissionEntry,bool){
 rows:=c.permissionRows()
 var both PermissionEntry
 hasBoth:=false
 for _,p:=range rows{
  if !strings.EqualFold(p.Name,e.Username){continue}
  if p.Platform==e.Platform{return p,true}
  if p.Platform=="all"{both=p;hasBoth=true}
 }
 return both,hasBoth
}
func (a *App) isBlacklisted(e live.Event)bool{
 p,ok:=a.config.effectiveEntry(e)
 if ok{return p.Role=="blacklist"}
 if a.config.Permissions!=nil{return false}
 return named(a.config.Blacklist,e.Username)||named(a.config.Blacklist,e.UserID)
}
func (a *App) commandAllowed(e live.Event,cap string)bool{
 if a.isBlacklisted(e){return false}
 if e.Platform=="bilibili"&&e.IsAnchor{return true}
 p,ok:=a.config.effectiveEntry(e)
 if ok{
  switch p.Role{
  case "super_admin":return true
  case "admin","part_time":return containsCap(p.Capabilities,cap)
  default:return false
  }
 }
 if a.config.Permissions!=nil{return e.Platform=="bilibili"&&e.IsRoomAdmin&&a.config.Switches!=nil&&a.config.Switches.RoomAdminOperator}
 return named(a.config.SuperAdmins,e.Username)||named(a.config.Admins,e.Username)||
  (e.Platform=="bilibili"&&e.IsRoomAdmin&&a.config.Switches!=nil&&a.config.Switches.RoomAdminOperator)
}
func (a *App) upsertRoleLocked(platform,name,role string)bool{
 name=strings.TrimSpace(name)
 if name==""||utf8.RuneCountInString(name)>60{return false}
 if a.config.Permissions==nil{a.config.Permissions=legacyPermissions(a.config)}
 for i,p:=range a.config.Permissions{
  if p.Platform==platform&&strings.EqualFold(p.Name,name){
   if p.Role==role{return false}
   a.config.Permissions[i].Role=role
   if role=="admin"{a.config.Permissions[i].Capabilities=copyCaps(adminDefaults)}else{a.config.Permissions[i].Capabilities=nil}
   return true
  }
 }
 p:=PermissionEntry{Name:name,Platform:platform,Role:role}
 if role=="admin"{p.Capabilities=copyCaps(adminDefaults)}
 a.config.Permissions=append(a.config.Permissions,p)
 return true
}
func (a *App) removeRoleLocked(platform,name,role string)bool{
 if a.config.Permissions==nil{a.config.Permissions=legacyPermissions(a.config)}
 for i,p:=range a.config.Permissions{
  if (p.Platform==platform||p.Platform=="all")&&strings.EqualFold(p.Name,name)&&p.Role==role{
   a.config.Permissions=append(a.config.Permissions[:i],a.config.Permissions[i+1:]...)
   return true
  }
 }
 return false
}
