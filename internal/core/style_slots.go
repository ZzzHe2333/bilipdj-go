package core

import (
 "encoding/json"
 "fmt"
 "net/http"
 "strconv"
)

// Ten OBS style presets are independent from the ten QUEUE archive slots.
// Stored with state.json, each preset is private to the Go Web variant.
func (a *App) styleSlotRoutes(mux *http.ServeMux) {
 mux.HandleFunc("GET /api/style/slots",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  a.mu.RLock(); defer a.mu.RUnlock()
  slots:=make([]map[string]any,0,10)
  for n:=1;n<=10;n++ {
   _,ok:=a.styleSlots[strconv.Itoa(n)]
   slots=append(slots,map[string]any{"slot":n,"saved":ok})
  }
  send(w,200,slots)
 })
 mux.HandleFunc("GET /api/style/slots/{slot}",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  n,e:=strconv.Atoi(r.PathValue("slot"))
  if e!=nil||n<1||n>10{send(w,400,map[string]string{"error":"样式存档只能是 1–10"});return}
  a.mu.RLock();saved,ok:=a.styleSlots[strconv.Itoa(n)];a.mu.RUnlock()
  if !ok{send(w,404,map[string]string{"error":"此样式存档尚未保存"});return}
  send(w,200,map[string]any{"slot":n,"style":saved})
 })
 mux.HandleFunc("POST /api/style/slots/{slot}",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  n,e:=strconv.Atoi(r.PathValue("slot"))
  if e!=nil||n<1||n>10{send(w,400,map[string]string{"error":"样式存档只能是 1–10"});return}
  input:=map[string]any{}
  if e=decode(r,&input);e!=nil{send(w,400,map[string]string{"error":e.Error()});return}
  raw,e:=json.Marshal(input)
  if e!=nil||len(raw)>32000||len(input)>120{send(w,400,map[string]string{"error":"样式存档过大"});return}
  a.mu.Lock()
  previous,exists:=a.styleSlots[strconv.Itoa(n)]
  if a.styleSlots==nil{a.styleSlots=make(map[string]map[string]any)}
  a.styleSlots[strconv.Itoa(n)]=input
  e=a.saveLocked()
  if e!=nil {
   if exists{a.styleSlots[strconv.Itoa(n)]=previous}else{delete(a.styleSlots,strconv.Itoa(n))}
  }
  a.mu.Unlock()
  if e!=nil{send(w,500,map[string]string{"error":e.Error()});return}
  send(w,200,map[string]any{"status":"ok","slot":n,"message":fmt.Sprintf("样式存档 %d 已保存",n)})
 })
 mux.HandleFunc("POST /api/style/slots/{slot}/apply",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  n,e:=strconv.Atoi(r.PathValue("slot"))
  if e!=nil||n<1||n>10{send(w,400,map[string]string{"error":"样式存档只能是 1–10"});return}
  a.mu.Lock()
  stored,ok:=a.styleSlots[strconv.Itoa(n)]
  if !ok{a.mu.Unlock();send(w,404,map[string]string{"error":"该样式存档尚未保存"});return}
  cloned:=make(map[string]any,len(stored))
  for k,v:=range stored{cloned[k]=v}
  raw,e:=json.Marshal(cloned)
  if e!=nil{a.mu.Unlock();send(w,500,map[string]string{"error":"样式编码失败"});return}
  before:=a.style
  a.style=cloned
  e=a.saveLocked()
  if e==nil{e=a.writeWebAppearanceFile("style",raw)}
  if e!=nil{a.style=before}else{a.publishLocked(Event{Type:"style",Data:cloned})}
  a.mu.Unlock()
  if e!=nil{send(w,500,map[string]string{"error":e.Error()});return}
  send(w,200,map[string]any{"status":"ok","slot":n,"style":cloned})
 })
}
