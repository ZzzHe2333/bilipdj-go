package core

import (
 "encoding/json"
 "fmt"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// liveSnapshot is captured while a.mu is held. All state changed by danmu,
// gifts and legacy import is restored on persistence failure, including
// permissions, daily quotas, gifts and both queue storage locations.
type liveSnapshot struct {
 queue queueSnapshot
 config Config
 dailyPeriod string
 dailyCounts map[string]int
 giftCredits map[string]int
 giftUsed map[string]bool
 giftSeen []string
 giftLast *live.Event
 style map[string]any
 appearance map[string]any
}

func copyMap[V any](src map[string]V) map[string]V {
 if src==nil{return nil}
 dst:=make(map[string]V,len(src))
 for k,v:=range src{dst[k]=v}
 return dst
}

func deepCopyConfig(c Config)(Config,error){
 raw,err:=json.Marshal(c)
 if err!=nil{return Config{},err}
 var cloned Config
 if err=json.Unmarshal(raw,&cloned);err!=nil{return Config{},err}
 return cloned,nil
}

func (a *App) liveSnapshotLocked()(liveSnapshot,error){
 cfg,err:=deepCopyConfig(a.config)
 if err!=nil{return liveSnapshot{},fmt.Errorf("无法建立直播事务配置快照: %w",err)}
 return liveSnapshot{
  queue:a.queueSnapshotLocked(),
  config:cfg,
  dailyPeriod:a.dailyPeriod,
  dailyCounts:copyMap(a.dailyCounts),
  giftCredits:copyMap(a.giftCredits),
  giftUsed:copyMap(a.giftUsed),
  giftSeen:append([]string(nil),a.giftSeen...),
  giftLast:a.giftLast,
  style:copyMap(a.style),
  appearance:copyMap(a.appearance),
 },nil
}

func (a *App) restoreLiveLocked(s liveSnapshot)error {
 a.config=s.config
 a.dailyPeriod=s.dailyPeriod
 a.dailyCounts=s.dailyCounts
 a.giftCredits=s.giftCredits
 a.giftUsed=s.giftUsed
 a.giftSeen=s.giftSeen
 a.giftLast=s.giftLast
 a.style=s.style
 a.appearance=s.appearance
 return a.restoreQueueLocked(s.queue)
}
