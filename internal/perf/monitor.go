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

// Unavailable measurements are not reported as a genuine zero.
type Metric struct {
 Available bool `json:"available"`
 Value float64 `json:"value"`
 Unit string `json:"unit"`
 Scope string `json:"scope"`
 Note string `json:"note,omitempty"`
}

// Exactly seven metrics. No machine CPU, network, GPU, or NPU collection.
type Snapshot struct {
 At time.Time `json:"at"`
 CPU Metric `json:"cpu"`
 Memory Metric `json:"memory"`
 DataDisk Metric `json:"data_disk"`
 DiskRead Metric `json:"disk_read"`
 DiskWrite Metric `json:"disk_write"`
 ProjectDisk Metric `json:"project_disk"`
 ArchiveDisk Metric `json:"archive_disk"`
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

type sizeCache struct {
 bytes uint64
 measuredAt time.Time
 available bool
 note string
}

type Monitor struct {
 mu sync.Mutex
 dataDir string
 archiveDir string
 executable string
 previousAt time.Time
 prev counters
 prevValid bool
 dataCache sizeCache
 projectCache sizeCache
 archiveCache sizeCache
}

func New(dataDir string) *Monitor {
 executable, _ := os.Executable()
 return &Monitor{dataDir:dataDir, executable:executable}
}

// ArchiveDir must come from the resolved storage plan. Windows usually places
// archives in LocalAppData, outside its Roaming configuration directory.
func (m *Monitor) SetArchiveDir(path string) {
 m.mu.Lock()
 defer m.mu.Unlock()
 if m.archiveDir != path {
  m.archiveDir=path
  m.archiveCache=sizeCache{}
 }
}

func value(n float64, unit, scope, note string) Metric {
 if math.IsNaN(n)||math.IsInf(n,0)||n<0{return missing(unit,scope,"读数无效")}
 return Metric{Available:true,Value:n,Unit:unit,Scope:scope,Note:note}
}
func missing(unit,scope,reason string) Metric {
 return Metric{Available:false,Unit:unit,Scope:scope,Note:reason}
}

// A bounded directory scan follows neither symlinks nor mount-like directory
// indirections. Files deleted mid-scan are not treated as valid zero.
var errScanLimit=errors.New("scan limit exceeded")
func dataSize(root string,maxFiles int,limit time.Duration)(uint64,error) {
 start:=time.Now()
 var total uint64
 count:=0
 err:=filepath.WalkDir(root,func(path string,d fs.DirEntry,walkErr error)error{
  if walkErr!=nil{return walkErr}
  count++
  if count>maxFiles||time.Since(start)>limit{return errScanLimit}
  if d.Type()&os.ModeSymlink!=0{return nil}
  if !d.Type().IsRegular(){return nil}
  info,err:=d.Info()
  if err!=nil{return err}
  if info.Size()>0{total+=uint64(info.Size())}
  return nil
 })
 return total,err
}

// A missing archive directory on a first run represents zero stored archives.
// Other missing/unreadable directories must remain unavailable, not zero.
func folderSize(root string,missingIsZero bool)(uint64,error) {
 if root=="" { return 0,os.ErrNotExist }
 n,err:=dataSize(root,10000,150*time.Millisecond)
 if missingIsZero&&errors.Is(err,os.ErrNotExist){return 0,nil}
 return n,err
}

// Project footprint = the running executable plus recognizable files shipped
// next to it. Do NOT sum the entire parent directory: users often put the EXE
// alongside other unrelated software, which would badly inflate this metric.
// All Vue/OBS assets are embedded in the Go binary.
func packageSize(executable string)(uint64,error) {
 if executable==""{return 0,os.ErrNotExist}
 resolved,err:=filepath.EvalSymlinks(executable)
 if err!=nil{return 0,err}
 info,err:=os.Stat(resolved)
 if err!=nil{return 0,err}
 if !info.Mode().IsRegular(){return 0,errors.New("not a regular executable")}
 total:=uint64(info.Size())
 directory:=filepath.Dir(resolved)
 for _,filename:=range []string{
  "bilipdj-go-mcp.exe","bilipdj-go.ico","bilipdj-go-tray.ico",
  "README.md","LICENSE","NOTICE.md","update-manifest.json",
  "docs","third_party",
 } {
  entry:=filepath.Join(directory,filename)
  fi,e:=os.Lstat(entry)
  if errors.Is(e,os.ErrNotExist){continue}
  if e!=nil{return 0,e}
  if fi.Mode()&os.ModeSymlink!=0 {continue}
  if fi.IsDir() {
   n,e:=folderSize(entry,false)
   if e!=nil{return 0,e}
   total+=n
  } else if fi.Mode().IsRegular()&&fi.Size()>0 {
   total+=uint64(fi.Size())
  }
 }
 return total,nil
}

func (m *Monitor) refreshSizes(now time.Time,cache *sizeCache,scan func()(uint64,error)) {
 if !cache.measuredAt.IsZero()&&now.Sub(cache.measuredAt)<time.Minute{return}
 size,err:=scan()
 cache.measuredAt=now
 cache.available=err==nil
 if err==nil {
  cache.bytes=size
  cache.note=""
 } else {
  cache.note="文件不可读取、超过 10000 个目录条目或扫描超时"
 }
}

func (m *Monitor) Sample() Snapshot {
 m.mu.Lock()
 defer m.mu.Unlock()
 now:=time.Now()
 out:=Snapshot{At:now,
  CPU:missing("%","本项目进程","正在获取采样基线"),
  Memory:missing("B","本项目进程内存","当前平台未提供"),
  DataDisk:missing("B","Go 数据配置目录","尚未测量"),
  DiskRead:missing("B/s","本项目进程 I/O 读取","当前平台未提供"),
  DiskWrite:missing("B/s","本项目进程 I/O 写入","当前平台未提供"),
  ProjectDisk:missing("B","本程序及发行包文件","尚未测量"),
  ArchiveDisk:missing("B","Go 排队存档目录","尚未测量"),
 }
 c,err:=readProcess()
 if err==nil {
  out.Memory=value(float64(c.MemoryBytes),"B","本项目进程",c.MemoryNote)
 }
 if m.prevValid&&err==nil {
  elapsed:=now.Sub(m.previousAt).Seconds()
  if elapsed>0 {
   if c.CPUSeconds>=m.prev.CPUSeconds {
    pct:=100*(c.CPUSeconds-m.prev.CPUSeconds)/elapsed/float64(runtime.NumCPU())
    out.CPU=value(math.Min(100,pct),"%","本项目进程","占所有逻辑 CPU 总计算能力的百分比")
   }
   if c.DiskAvailable&&m.prev.DiskAvailable {
    if c.DiskReadBytes>=m.prev.DiskReadBytes {
     out.DiskRead=value(float64(c.DiskReadBytes-m.prev.DiskReadBytes)/elapsed,"B/s","本项目进程 I/O 读取",c.DiskNote)
    }
    if c.DiskWriteBytes>=m.prev.DiskWriteBytes {
     out.DiskWrite=value(float64(c.DiskWriteBytes-m.prev.DiskWriteBytes)/elapsed,"B/s","本项目进程 I/O 写入",c.DiskNote)
    }
   }
  }
 }
 if err!=nil {
  out.CPU.Note=err.Error()
  out.Memory.Note=err.Error()
  out.DiskRead.Note=err.Error()
  out.DiskWrite.Note=err.Error()
 } else if !c.DiskAvailable {
  out.DiskRead.Note="当前系统未提供进程级磁盘 I/O"
  out.DiskWrite.Note="当前系统未提供进程级磁盘 I/O"
 }
 m.refreshSizes(now,&m.dataCache,func()(uint64,error){return folderSize(m.dataDir,true)})
 m.refreshSizes(now,&m.projectCache,func()(uint64,error){return packageSize(m.executable)})
 m.refreshSizes(now,&m.archiveCache,func()(uint64,error){return folderSize(m.archiveDir,true)})
 if m.dataCache.available {
  out.DataDisk=value(float64(m.dataCache.bytes),"B","Go 数据配置目录","最多每 60 秒统计一次；若存档位于子目录，该值包含存档")
 } else {out.DataDisk.Note=m.dataCache.note}
 if m.projectCache.available {
  out.ProjectDisk=value(float64(m.projectCache.bytes),"B","本程序及发行包文件","可执行文件及同目录内识别到的配套文件；不含用户数据")
 } else {out.ProjectDisk.Note=m.projectCache.note}
 if m.archiveCache.available {
  out.ArchiveDisk=value(float64(m.archiveCache.bytes),"B","Go 排队存档目录","实际存档目录（含内部子目录），未创建时为 0；最多每 60 秒统计")
 } else if m.archiveDir=="" {
  out.ArchiveDisk.Note="正在定位存档目录"
 } else {out.ArchiveDisk.Note=m.archiveCache.note}
 m.previousAt=now
 m.prev=c
 m.prevValid=err==nil
 return out
}
