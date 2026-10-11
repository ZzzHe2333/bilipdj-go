package core

import "fmt"

// persistWebSettingsLocked keeps state.json and the separately mirrored
// style-web.json / appearance-web.json consistent when either write fails.
// Callers MUST hold a.mu. Do not publish a live OBS update until both
// writes succeed. If the second write fails, restore the previous state.json
// rather than leaving a pending style that would become active on restart.
func (a *App) persistWebSettingsLocked(kind string,obj map[string]any,raw []byte)error{
 previousStyle,previousAppearance:=a.style,a.appearance
 if kind=="style"{a.style=obj}else{a.appearance=obj}
 if err:=a.saveLocked();err!=nil{
  a.style,a.appearance=previousStyle,previousAppearance
  return fmt.Errorf("保存应用状态失败: %w",err)
 }
 if err:=a.writeWebAppearanceFile(kind,raw);err!=nil{
  a.style,a.appearance=previousStyle,previousAppearance
  if rollbackErr:=a.saveLocked();rollbackErr!=nil{
   return fmt.Errorf("写入 %s 文件失败: %v；恢复原状态也失败: %w",kind,err,rollbackErr)
  }
  return fmt.Errorf("写入 %s 文件失败（状态已回滚）: %w",kind,err)
 }
 if kind=="style"{a.publishLocked(Event{Type:"style",Data:obj})}
 return nil
}
