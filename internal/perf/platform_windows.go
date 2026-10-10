//go:build windows

package perf

import (
 "fmt"
 "syscall"
 "unsafe"
)

var (
 kernel32=syscall.NewLazyDLL("kernel32.dll")
 psapi=syscall.NewLazyDLL("psapi.dll")
 getProcessIoCounters=kernel32.NewProc("GetProcessIoCounters")
 getProcessMemoryInfo=psapi.NewProc("GetProcessMemoryInfo")
)

type processMemoryCounters struct {
 CB uint32
 PageFaultCount uint32
 PeakWorkingSetSize uintptr
 WorkingSetSize uintptr
 QuotaPeakPagedPoolUsage uintptr
 QuotaPagedPoolUsage uintptr
 QuotaPeakNonPagedPoolUsage uintptr
 QuotaNonPagedPoolUsage uintptr
 PagefileUsage uintptr
 PeakPagefileUsage uintptr
}

type processIO struct {
 ReadOperationCount uint64
 WriteOperationCount uint64
 OtherOperationCount uint64
 ReadTransferCount uint64
 WriteTransferCount uint64
 OtherTransferCount uint64
}

func fileTimeTicks(t syscall.Filetime)uint64 {
 return uint64(t.HighDateTime)<<32|uint64(t.LowDateTime)
}
func readProcess()(c counters,err error){
 handle,e:=syscall.GetCurrentProcess()
 if e!=nil{return c,e}
 var created,exited,kernel,user syscall.Filetime
 if err=syscall.GetProcessTimes(handle,&created,&exited,&kernel,&user);err!=nil{return}
 c.CPUSeconds=float64(fileTimeTicks(kernel)+fileTimeTicks(user))/1e7
 var mem processMemoryCounters
 mem.CB=uint32(unsafe.Sizeof(mem))
 r,_,callErr:=getProcessMemoryInfo.Call(uintptr(handle),uintptr(unsafe.Pointer(&mem)),uintptr(mem.CB))
 if r==0{return c,fmt.Errorf("GetProcessMemoryInfo: %v",callErr)}
 c.MemoryBytes=uint64(mem.WorkingSetSize)
 c.MemoryNote="当前进程物理工作集（Windows Working Set）"
 var io processIO
 ok,_,_:=getProcessIoCounters.Call(uintptr(handle),uintptr(unsafe.Pointer(&io)))
 if ok!=0 {
  c.DiskReadBytes=io.ReadTransferCount
  c.DiskWriteBytes=io.WriteTransferCount
  c.DiskAvailable=true
  c.DiskNote="Windows GetProcessIoCounters I/O 字节增量，可能包含缓存/设备 I/O，非纯物理磁盘吞吐"
 }
 return c,nil
}
