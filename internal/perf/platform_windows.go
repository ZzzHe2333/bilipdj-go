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
 getSystemTimes=kernel32.NewProc("GetSystemTimes")
 getProcessMemoryInfo=psapi.NewProc("GetProcessMemoryInfo")
 iphlpapi=syscall.NewLazyDLL("iphlpapi.dll")
 getIfTable=iphlpapi.NewProc("GetIfTable")
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

// Windows MIB_IFROW contains 32-bit octet counters. We handle wrap-around at
// the sampling layer and identify these as HOST network statistics.
type ifRow struct {
 Name [256]uint16
 Index uint32
 Type uint32
 MTU uint32
 Speed uint32
 PhysAddrLen uint32
 PhysAddr [8]byte
 AdminStatus uint32
 OperStatus uint32
 LastChange uint32
 InOctets uint32
 InUcastPkts uint32
 InNUcastPkts uint32
 InDiscards uint32
 InErrors uint32
 InUnknownProtos uint32
 OutOctets uint32
 OutUcastPkts uint32
 OutNUcastPkts uint32
 OutDiscards uint32
 OutErrors uint32
 OutQLen uint32
 DescrLen uint32
 Descr [256]byte
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

func readMachine()(m machineCounters){
 var idle,kernel,user syscall.Filetime
 r,_,_:=getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)),uintptr(unsafe.Pointer(&kernel)),uintptr(unsafe.Pointer(&user)))
 if r!=0 {
  total:=fileTimeTicks(kernel)+fileTimeTicks(user)
  idleTicks:=fileTimeTicks(idle)
  if total>=idleTicks {
   m.CPUTotal=total
   m.CPUBusy=total-idleTicks
   m.CPUAvailable=true
  }
 }
 // GetIfTable gives interface-wide traffic (32-bit counters). Return only
 // while all NIC values are below their wrap limit; a reset is invalidated
 // by the Monitor rather than producing a negative or fabricated rate.
 const insufficientBuffer=122
 size:=uint32(0)
 code,_,_:=getIfTable.Call(0,uintptr(unsafe.Pointer(&size)),0)
 if code!=insufficientBuffer||size<4||size>8<<20{return m}
 buf:=make([]byte,size)
 code,_,_=getIfTable.Call(uintptr(unsafe.Pointer(&buf[0])),uintptr(unsafe.Pointer(&size)),0)
 if code!=0{return m}
 count:=*(*uint32)(unsafe.Pointer(&buf[0]))
 rowSize:=int(unsafe.Sizeof(ifRow{}))
 if count>1024||int(count)*rowSize+4>len(buf){return m}
 for i:=uint32(0);i<count;i++{
  row:=(*ifRow)(unsafe.Pointer(&buf[4+int(i)*rowSize]))
  if row.Type==24||row.OperStatus!=5{continue} // 24 loopback; 5 operational
  m.RX+=uint64(row.InOctets)
  m.TX+=uint64(row.OutOctets)
 }
 if count>0{
  m.NetworkAvailable=true
  m.NetworkScope="整机网络接口"
  m.NetworkNote="包括其他进程；Windows 旧网卡接口计数器可能在 4GB 后回绕"
 }
 return m
}
