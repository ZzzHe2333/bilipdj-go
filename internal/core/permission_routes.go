package core

import "net/http"

func (a *App) permissionRoutes(mux *http.ServeMux){
 mux.HandleFunc("GET /api/permissions",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  a.mu.RLock()
  list:=a.config.permissionRows()
  a.mu.RUnlock()
  send(w,200,map[string]any{"entries":list,"capabilities":[]map[string]string{
   {"id":"moderate","label":"拉黑/取消拉黑"},
   {"id":"queue","label":"增删/完成队列"},
   {"id":"insert","label":"插队命令"},
   {"id":"switch","label":"排队功能开关"},
   {"id":"limits","label":"修改人数上限"},
  }})
 })
 mux.HandleFunc("POST /api/permissions",func(w http.ResponseWriter,r *http.Request){
  if !a.isAdmin(r){send(w,403,map[string]string{"error":"forbidden"});return}
  var request struct {Entries []PermissionEntry `json:"entries"`}
  if err:=decodeLimit(r,&request,128<<10);err!=nil{send(w,400,map[string]string{"error":err.Error()});return}
  if request.Entries==nil{send(w,400,map[string]string{"error":"entries 必须为数组，空名单请提交 []"});return}
  entries,err:=cleanPermissions(request.Entries)
  if err!=nil{send(w,400,map[string]string{"error":err.Error()});return}
  a.mu.Lock()
  original:=a.config.Permissions
  a.config.Permissions=entries
  err=a.saveLocked()
  if err!=nil{a.config.Permissions=original}
  a.mu.Unlock()
  if err!=nil{send(w,500,map[string]string{"error":"保存权限失败："+err.Error()});return}
  send(w,200,map[string]any{"status":"ok","entries":entries})
 })
}
