package perf

import (
 "encoding/json"
 "math"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"
 "time"
)

func write(t *testing.T,path,text string){
 t.Helper()
 if err:=os.WriteFile(path,[]byte(text),0600);err!=nil{t.Fatal(err)}
}

func TestMetricAvailabilityIsNotZero(t *testing.T) {
 m:=missing("B","project","not supported")
 if m.Available||m.Value!=0||m.Note==""{t.Fatalf("missing metric: %+v",m)}
 v:=value(0,"B","archive","")
 if !v.Available||v.Value!=0{t.Fatalf("real zero must be available: %+v",v)}
 if value(math.NaN(),"%","cpu","").Available||value(-1,"B","disk","").Available {
  t.Fatal("invalid metrics must be unavailable")
 }
}

func TestBoundedDirectoryScanAndSymlink(t *testing.T){
 dir:=t.TempDir()
 write(t,filepath.Join(dir,"one"),"abc")
 if err:=os.Mkdir(filepath.Join(dir,"nested"),0700);err!=nil{t.Fatal(err)}
 write(t,filepath.Join(dir,"nested","two"),"12345")
 outside:=filepath.Join(t.TempDir(),"outside")
 write(t,outside,strings.Repeat("x",300))
 _=os.Symlink(outside,filepath.Join(dir,"link"))
 size,err:=dataSize(dir,100,time.Second)
 if err!=nil||size!=8{t.Fatalf("dir size %d: %v",size,err)}
 if _,err=dataSize(dir,1,time.Second);err==nil{t.Fatal("ignored file budget")}
 if _,err=dataSize(dir,100,-time.Second);err==nil{t.Fatal("ignored time budget")}
}

func TestProjectFootprintExcludesUnrelatedFiles(t *testing.T){
 dir:=t.TempDir()
 exe:=filepath.Join(dir,"bilipdj-go.exe")
 write(t,exe,"binary")
 write(t,filepath.Join(dir,"bilipdj-go-mcp.exe"),"helper")
 write(t,filepath.Join(dir,"README.md"),"readme")
 write(t,filepath.Join(dir,"someone-else.exe"),strings.Repeat("x",10000))
 if err:=os.Mkdir(filepath.Join(dir,"docs"),0700);err!=nil{t.Fatal(err)}
 write(t,filepath.Join(dir,"docs","file.md"),"documentation")
 outside:=filepath.Join(t.TempDir(),"huge")
 write(t,outside,strings.Repeat("x",1000))
 _=os.Symlink(outside,filepath.Join(dir,"NOTICE.md"))
 size,err:=packageSize(exe)
 expected:=len("binary")+len("helper")+len("readme")+len("documentation")
 if err!=nil||size!=uint64(expected){t.Fatalf("project files=%d, want %d (err %v)",size,expected,err)}
}

func TestSevenKeysAndStorageSource(t *testing.T){
 base:=t.TempDir()
 cfg:=filepath.Join(base,"config")
 archive:=filepath.Join(base,"archive")
 if err:=os.Mkdir(cfg,0700);err!=nil{t.Fatal(err)}
 if err:=os.Mkdir(archive,0700);err!=nil{t.Fatal(err)}
 write(t,filepath.Join(cfg,"state.json"),"abcdefg")
 write(t,filepath.Join(archive,"go-queue-state.json"),"1234567890")
 exe:=filepath.Join(base,"bilipdj-go")
 write(t,exe,"runtime")
 m:=New(cfg)
 m.executable=exe
 m.SetArchiveDir(archive)
 one:=m.Sample()
 if !one.DataDisk.Available||one.DataDisk.Value!=7 {t.Fatalf("config %+v",one.DataDisk)}
 if !one.ArchiveDisk.Available||one.ArchiveDisk.Value!=10{t.Fatalf("archive %+v",one.ArchiveDisk)}
 if !one.ProjectDisk.Available||one.ProjectDisk.Value!=7{t.Fatalf("project %+v",one.ProjectDisk)}
 raw,err:=json.Marshal(one)
 if err!=nil{t.Fatal(err)}
 var payload map[string]json.RawMessage
 if err=json.Unmarshal(raw,&payload);err!=nil{t.Fatal(err)}
 expect:=[]string{"at","cpu","memory","data_disk","disk_read","disk_write","project_disk","archive_disk"}
 actual:=make([]string,0,len(payload))
 for k:=range payload{actual=append(actual,k)}
 if len(actual)!=len(expect) {t.Fatalf("unexpected metric keys %v",actual)}
 for _,k:=range expect{if _,ok:=payload[k];!ok{t.Fatalf("missing key %s",k)}}
 // Initial caching should not rescan the archive, even when its files grow.
 write(t,filepath.Join(archive,"go-queue-state.json"),strings.Repeat("a",50))
 two:=m.Sample()
 if two.ArchiveDisk.Value!=10{t.Fatalf("archive should be cached: %+v",two.ArchiveDisk)}
 // Changing the resolved archive location invalidates its cache.
 changed:=filepath.Join(base,"fresh-archive")
 m.SetArchiveDir(changed)
 three:=m.Sample()
 if !three.ArchiveDisk.Available||three.ArchiveDisk.Value!=0{t.Fatalf("uncreated archive should mean zero: %+v",three.ArchiveDisk)}
 if reflect.DeepEqual(one.ArchiveDisk,three.ArchiveDisk){t.Fatal("archive location change did not invalidate cache")}
}

func TestProjectAndArchiveErrorsAreNotZero(t *testing.T){
 m:=New(t.TempDir())
 m.executable=filepath.Join(t.TempDir(),"missing-binary")
 m.SetArchiveDir(filepath.Join(t.TempDir(),"missing-archives"))
 sample:=m.Sample()
 if sample.ProjectDisk.Available||sample.ProjectDisk.Note==""{t.Fatalf("missing binary: %+v",sample.ProjectDisk)}
 if !sample.ArchiveDisk.Available||sample.ArchiveDisk.Value!=0{t.Fatalf("empty archives %+v",sample.ArchiveDisk)}
}
