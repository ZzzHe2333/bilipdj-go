package perf

import (
 "errors"
 "io/fs"
 "math"
 "os"
 "path/filepath"
 "runtime"
 "sync"
 "time"
)

// Metric distinguishes an actual zero from an unavailable measurement.
// Scope identifies whether the figures are per-process, process data files,
// or machine/container wide.
type Metric struct {
 Available bool `json:"available"`
 Value float64 `json:"value"`
 Unit string `json:"unit"`
 Scope string `json:"scope"`
 Note string `json:"note,omitempty"`
}

type Snapshot struct {
 At time.Time `json:"at"`
 CPU Metric `json:"cpu"`
 SystemCPU Metric `json:"system_cpu"`
 Memory Metric `json:"memory"`
 DataDisk Metric `json:"data_disk"`
 DiskRead Metric `json:"disk_read"`
 DiskWrite Metric `json:"disk_write"`
 NetworkReceive Metric `json:"network_receive"`
 NetworkSend Metric `json:"network_send"`
 GPU Metric `json:"gpu"`
 NPU Metric `json:"npu"`
}

type counters struct {
 CPUSeconds float64
 MemoryBytes uint64
 MemoryNote string
 DiskReadBytes uint64
 DiskWriteBytes uint64
 DiskAvailable bool
 DiskNote string
}

type machineCounters struct {
 CPUBusy, CPUTotal uint64
 CPUAvailable bool
 RX, TX uint64
 NetworkAvailable bool
 NetworkScope string
 NetworkNote string
}

type Monitor struct {
 mu sync.Mutex
 dataDir string
 previousAt time.Time
 prev counters
 prevMachine machineCounters
 prevValid bool
 cachedDirBytes uint64
 dirMeasured time.Time
 dirAvailable bool
}

func New(dataDir string) *Monitor { return &Monitor{dataDir:dataDir} }

func value(n float64, unit, scope, note string) Metric {
 if math.IsNaN(n)||math.IsInf(n,0)||n<0 {return missing(unit,scope,"读数无效")}
 return Metric{Available:true,Value:n,Unit:unit,Scope:scope,Note:note}
}
func missing(unit, scope, reason string) Metric {
 return Metric{Available:false,Unit:unit,Scope:scope,Note:reason}
}

// No background goroutine: sampling happens ONLY in response to an authorized
// request from an actively viewing performance page. 0 interval => no calls.
func (m *Monitor) Sample() Snapshot {
 m.mu.Lock()
 defer m.mu.Unlock()
 now:=time.Now()
 out:=Snapshot{At:now,
  CPU:missing("%","本项目进程","正在获取采样基线"),
  SystemCPU:missing("%","整机 CPU","当前平台未提供"),
  Memory:missing("B","本项目进程 RSS","当前平台未提供"),
  DataDisk:missing("B","本项目数据目录","未测量"),
  DiskRead:missing("B/s","本项目进程磁盘读取","当前平台未提供"),
  DiskWrite:missing("B/s","本项目进程磁盘写入","当前平台未提供"),
  NetworkReceive:missing("B/s","整机/容器网络接口","当前平台未提供可信统计"),
  NetworkSend:missing("B/s","整机/容器网络接口","当前平台未提供可信统计"),
  GPU:missing("%","本项目 GPU","未接入可靠的进程级 GPU 性能计数器"),
  NPU:missing("%","本项目 NPU","操作系统没有统一的可信进程级 NPU 计数器"),
 }
 c,processErr:=readProcess()
 host:=readMachine()
 if processErr==nil {
  out.Memory=value(float64(c.MemoryBytes),"B","本项目进程",c.MemoryNote)
 }
 if host.CPUAvailable {out.SystemCPU=missing("%","整机 CPU","正在获取采样基线")}
 if host.NetworkAvailable {
  scope:=host.NetworkScope
  out.NetworkReceive=missing("B/s",scope,"正在获取采样基线；该值包含其他程序流量")
  out.NetworkSend=missing("B/s",scope,"正在获取采样基线；该值包含其他程序流量")
 }
 if m.prevValid {
  elapsed:=now.Sub(m.previousAt).Seconds()
  if elapsed>0 {
   if processErr==nil && c.CPUSeconds>=m.prev.CPUSeconds {
    // Matches the whole-machine percentage convention; 100% means every
    // logical CPU is busy running this process.
    pct:=100*(c.CPUSeconds-m.prev.CPUSeconds)/elapsed/float64(runtime.NumCPU())
    out.CPU=value(math.Min(100,pct),"%","本项目进程","占所有逻辑 CPU 总计算能力的百分比")
   }
   if processErr==nil&&c.DiskAvailable&&m.prev.DiskAvailable {
    if c.DiskReadBytes>=m.prev.DiskReadBytes {out.DiskRead=value(float64(c.DiskReadBytes-m.prev.DiskReadBytes)/elapsed,"B/s","本项目进程 I/O 读取",c.DiskNote)}
    if c.DiskWriteBytes>=m.prev.DiskWriteBytes {out.DiskWrite=value(float64(c.DiskWriteBytes-m.prev.DiskWriteBytes)/elapsed,"B/s","本项目进程 I/O 写入",c.DiskNote)}
   }
   if host.CPUAvailable&&m.prevMachine.CPUAvailable&&host.CPUTotal>m.prevMachine.CPUTotal&&host.CPUBusy>=m.prevMachine.CPUBusy {
    out.SystemCPU=value(100*float64(host.CPUBusy-m.prevMachine.CPUBusy)/float64(host.CPUTotal-m.prevMachine.CPUTotal),"%","整机 CPU","包含其他进程")
   }
   if host.NetworkAvailable&&m.prevMachine.NetworkAvailable&&host.RX>=m.prevMachine.RX&&host.TX>=m.prevMachine.TX {
    out.NetworkReceive=value(float64(host.RX-m.prevMachine.RX)/elapsed,"B/s",host.NetworkScope,host.NetworkNote)
    out.NetworkSend=value(float64(host.TX-m.prevMachine.TX)/elapsed,"B/s",host.NetworkScope,host.NetworkNote)
   }
  }
 }
 if processErr != nil {
  out.CPU.Note=processErr.Error()
  out.Memory.Note=processErr.Error()
  out.DiskRead.Note=processErr.Error()
  out.DiskWrite.Note=processErr.Error()
 } else if !c.DiskAvailable {
  out.DiskRead.Note="当前系统不提供可移植的进程级磁盘 I/O"
  out.DiskWrite.Note="当前系统不提供可移植的进程级磁盘 I/O"
 }
 // Directory scan is throttled to once a minute (and capped by time/files).
 // Never traverse symlinked directories; failure is displayed, not zeroed.
 if m.dirMeasured.IsZero() || now.Sub(m.dirMeasured)>=time.Minute {
  n,err:=dataSize(m.dataDir,10000,150*time.Millisecond)
  m.dirMeasured=now
  m.dirAvailable=err==nil
  if err==nil {m.cachedDirBytes=n}
 }
 if m.dirAvailable {
  out.DataDisk=value(float64(m.cachedDirBytes),"B","本项目数据目录","最多每 60 秒重新计算一次")
 } else {out.DataDisk.Note="目录太大、不可读取或扫描超时"}
 m.previousAt=now
 m.prev=c
 m.prevMachine=host
 m.prevValid=processErr==nil
 return out
}

var errScanLimit=errors.New("scan limit exceeded")
func dataSize(root string,maxFiles int,limit time.Duration)(uint64,error){
 start:=time.Now()
 var total uint64
 count:=0
 err:=filepath.WalkDir(root,func(path string,d fs.DirEntry,walkErr error)error{
  if walkErr!=nil{return walkErr}
  count++
  if count>maxFiles||time.Since(start)>limit{return errScanLimit}
  if d.Type()&os.ModeSymlink!=0{return nil}
  if !d.Type().IsRegular(){return nil}
  info,err:=d.Info();if err!=nil{return err}
  if info.Size()>0{total+=uint64(info.Size())}
  return nil
 })
 return total,err
}
