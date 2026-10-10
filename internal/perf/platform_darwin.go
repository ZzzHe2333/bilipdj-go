//go:build darwin

package perf

import (
 "runtime"
 "syscall"
)

func readProcess()(c counters,err error){
 var usage syscall.Rusage
 if err=syscall.Getrusage(syscall.RUSAGE_SELF,&usage);err!=nil{return}
 c.CPUSeconds=float64(usage.Utime.Sec+usage.Stime.Sec)+float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
 // macOS ru_maxrss is a PEAK; do not mislabel it as current RSS.
 c.MemoryBytes=uint64(usage.Maxrss)
 c.MemoryNote="macOS 报告的是峰值 RSS（非当前驻留内存）"
 if c.MemoryBytes==0 {
  var ms runtime.MemStats
  runtime.ReadMemStats(&ms)
  c.MemoryBytes=ms.HeapAlloc
  c.MemoryNote="Go 堆已分配内存（无法获取进程 RSS）"
 }
 return c,nil
}
func readMachine() machineCounters {return machineCounters{}}
