package perf

import (
 "math"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestMetricAvailabilityIsNotZero(t *testing.T) {
 m:=missing("%","本项目 NPU","无统一进程计数器")
 if m.Available||m.Value!=0||m.Note==""{t.Fatalf("unavailable must be marked: %+v",m)}
 v:=value(0,"%","本项目 CPU","真实零值")
 if !v.Available||v.Value!=0{t.Fatalf("real zero must be available: %+v",v)}
 if value(math.NaN(),"%","cpu","").Available||value(-1,"B","disk","").Available {
  t.Fatal("invalid metrics must be missing")
 }
}

func TestDataDirectorySizeIsBoundedAndSkipsSymlinks(t *testing.T) {
 dir:=t.TempDir()
 if e:=os.WriteFile(filepath.Join(dir,"one.txt"),[]byte("abc"),0600);e!=nil{t.Fatal(e)}
 if e:=os.Mkdir(filepath.Join(dir,"nested"),0700);e!=nil{t.Fatal(e)}
 if e:=os.WriteFile(filepath.Join(dir,"nested","two"),[]byte("12345"),0600);e!=nil{t.Fatal(e)}
 outside:=filepath.Join(t.TempDir(),"outside")
 if e:=os.WriteFile(outside,[]byte(strings.Repeat("x",300)),0600);e!=nil{t.Fatal(e)}
 _=os.Symlink(outside,filepath.Join(dir,"link"))
 n,err:=dataSize(dir,100,time.Second)
 if err!=nil||n!=8{t.Fatalf("size=%d err=%v",n,err)}
 if _,err=dataSize(dir,1,time.Second);err==nil{t.Fatal("max walk items ignored")}
 if _,err=dataSize(dir,100,-1*time.Second);err==nil{t.Fatal("time bound ignored")}
}

func TestSnapshotStatesAndCache(t *testing.T) {
 dir:=t.TempDir()
 if e:=os.WriteFile(filepath.Join(dir,"state.json"),[]byte("initial"),0600);e!=nil{t.Fatal(e)}
 mon:=New(dir)
 first:=mon.Sample()
 if first.At.IsZero()||first.NPU.Available||first.GPU.Available {
  t.Fatalf("invalid snapshot %+v",first)
 }
 if !first.DataDisk.Available||first.DataDisk.Value!=7{t.Fatalf("data disk=%+v",first.DataDisk)}
 _=os.WriteFile(filepath.Join(dir,"state.json"),[]byte("updated larger"),0600)
 second:=mon.Sample()
 if !second.DataDisk.Available||second.DataDisk.Value!=7{
  t.Fatalf("directory size must be cached for one minute: %+v",second.DataDisk)
 }
 if second.NetworkReceive.Available&&!strings.Contains(second.NetworkReceive.Scope,"整机")&&!strings.Contains(second.NetworkReceive.Scope,"容器"){
  t.Fatalf("network must not claim per-process scope: %+v",second.NetworkReceive)
 }
}

func TestUnavailableNetworkAndNPUNeverFakeZero(t *testing.T) {
 s:=New(t.TempDir()).Sample()
 if s.NPU.Available||s.GPU.Available{t.Fatal("unimplemented NPU/GPU must not advertise zero usage")}
 if !s.NetworkReceive.Available&&s.NetworkReceive.Note==""{t.Fatal("unavailable network requires reason")}
}
