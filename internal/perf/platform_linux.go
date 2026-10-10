//go:build linux

package perf

import (
 "bufio"
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
func readMachine()(m machineCounters){
 data,e:=os.ReadFile("/proc/stat")
 if e==nil {
  line:=strings.SplitN(string(data),"\n",2)[0]
  p:=strings.Fields(line)
  if len(p)>=5&&p[0]=="cpu" {
   var nums []uint64
   for i,s:=range p[1:] {if i>=8{break};n,e:=strconv.ParseUint(s,10,64);if e!=nil{break};nums=append(nums,n)}
   if len(nums)>=4 {
    for _,n:=range nums{m.CPUTotal+=n}
    m.CPUBusy=m.CPUTotal-nums[3]
    if len(nums)>4{m.CPUBusy-=nums[4]} // iowait counts as idle
    m.CPUAvailable=true
   }
  }
 }
 f,e:=os.Open("/proc/net/dev")
 if e!=nil{return m}
 defer f.Close()
 scanner:=bufio.NewScanner(f)
 anyInterface:=false
 for scanner.Scan(){
  line:=strings.TrimSpace(scanner.Text())
  parts:=strings.SplitN(line,":",2)
  if len(parts)!=2||strings.TrimSpace(parts[0])=="lo"{continue}
  fields:=strings.Fields(parts[1])
  if len(fields)<16{continue}
  rx,e1:=strconv.ParseUint(fields[0],10,64)
  tx,e2:=strconv.ParseUint(fields[8],10,64)
  if e1!=nil||e2!=nil{continue}
  m.RX+=rx;m.TX+=tx;anyInterface=true
 }
 if scanner.Err()==nil&&anyInterface {
  m.NetworkAvailable=true
  m.NetworkScope="整机 / 容器网络接口"
  m.NetworkNote="主机或当前网络命名空间的所有非 loopback 流量；非本进程独占"
 }
 return m
}
