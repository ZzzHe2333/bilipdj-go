package core

import (
 "fmt"
)

// queueSnapshot is a value copy of the current queue, its archive slots and
// the active archive slot. The slot map must be copied because saveLocked
// mutates its current-slot entry before persisting to disk.
type queueSnapshot struct {
 queue []QueueItem
 slots map[string][]QueueItem
 active int
}
func (a *App) queueSnapshotLocked() queueSnapshot {
 snapshot:=queueSnapshot{
  queue:append([]QueueItem(nil),a.queue...),
  slots:make(map[string][]QueueItem,len(a.slots)),
  active:a.config.ArchiveSlot,
 }
 for k,entries:=range a.slots{
  snapshot.slots[k]=append([]QueueItem(nil),entries...)
 }
 return snapshot
}

// restoreQueueLocked reverses any mutation made while holding a.mu. A failed
// state.json write can occur after a successful external queue write, so a
// Local-backed queue also needs its previous on-disk contents restored.
func (a *App) restoreQueueLocked(s queueSnapshot)error{
 a.config.ArchiveSlot=s.active
 a.queue=append([]QueueItem{},s.queue...)
 a.slots=s.slots
 if a.queuePath!=""{
  if err:=a.saveQueueLocalLocked();err!=nil{
   return fmt.Errorf("无法恢复本地队列文件: %w",err)
  }
 }
 return nil
}
