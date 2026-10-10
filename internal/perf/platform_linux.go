//go:build linux

package perf

import (
 "bytes"
 "fmt"
 "os"
 "strconv"
 "strings"
 "syscall"
)

func readProcess()(c counters,err error){
 var usage syscall.Rusage
 if err=syscall.Getrusage(syscall.RUSAGE_SELF,&usage);err!=nil{return}
 c.CPUSeconds=float64(usage.Utime.Sec+usage.Stime.Sec)+float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
 status,e:=os.ReadFile("/proc/self/status")
 if e!=nil{return c,e}
 found:=false
 for _,l:=range bytes.Split(status,[]byte{'\n'}){
  if bytes.HasPrefix(l,[]byte("VmRSS:")){
   parts:=strings.Fields(string(l))
   if len(parts)>=2 {
    n,parseErr:=strconv.ParseUint(parts[1],10,64)
    if parseErr==nil {c.MemoryBytes=n*1024;found=true}
   }
   break
  }
 }
 if !found{return c,fmt.Errorf("VmRSS not available")}
 c.MemoryNote="当前常驻物理内存 RSS"
 ioData,e:=os.ReadFile("/proc/self/io")
 if e==nil {
  rd,wr:=false,false
  for _,l:=range strings.Split(string(ioData),"\n"){
   if strings.HasPrefix(l,"read_bytes:"){c.DiskReadBytes,_=strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(l,"read_bytes:")),10,64);rd=true}
   if strings.HasPrefix(l,"write_bytes:"){c.DiskWriteBytes,_=strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(l,"write_bytes:")),10,64);wr=true}
  }
  c.DiskAvailable=rd&&wr
  c.DiskNote="Linux /proc/self/io 的物理层读写字节增量"
 }
 return c,nil
}
