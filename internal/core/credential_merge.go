package core

// Only explicit (nonempty) cookies supplied by a configuration client are
// allowed to replace stored credentials. Empty/redacted values mean "keep the
// newest stored credential", also for secondary rooms keyed by immutable ID.
// A second merge must occur under App.mu immediately before commit because a
// QR login/logout may finish while cleanConfig is validating outside the lock.
func explicitCredentialFields(c Config)map[string]bool{
 flags:=map[string]bool{
  "bilibili":c.Bilibili.Cookie!="",
  "douyin":c.Douyin.Cookie!="",
  "huya":c.Huya.Cookie!="",
  "wechat_mp":c.WechatMP.Cookie!="",
  "kuaishou":c.Kuaishou.Cookie!="",
  "douyu":c.Douyu.Cookie!="",
 }
 for _,p:=range c.Listeners{flags[p.ID]=p.Cookie!=""}
 return flags
}
func mergeCurrentCredentials(c *Config,current Config,explicit map[string]bool){
 if !explicit["bilibili"]{c.Bilibili.Cookie=current.Bilibili.Cookie}
 if !explicit["douyin"]{c.Douyin.Cookie=current.Douyin.Cookie}
 if !explicit["huya"]{c.Huya.Cookie=current.Huya.Cookie}
 if !explicit["wechat_mp"]{c.WechatMP.Cookie=current.WechatMP.Cookie}
 if !explicit["kuaishou"]{c.Kuaishou.Cookie=current.Kuaishou.Cookie}
 if !explicit["douyu"]{c.Douyu.Cookie=current.Douyu.Cookie}
 currentIDs:=make(map[string]string,len(current.Listeners))
 for _,p:=range current.Listeners{currentIDs[p.ID]=p.Cookie}
 for i:=range c.Listeners{
  if !explicit[c.Listeners[i].ID]{
   c.Listeners[i].Cookie=currentIDs[c.Listeners[i].ID]
  }
 }
}
