package core

import (
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "time"
)

// diagnosticFiles persists sanitized application and collector logs into the
// same resolved user-data directory as state.json, not the install directory.
// No Cookie or credential body is ever deliberately logged.
type diagnosticFiles struct {
 mu sync.Mutex
 dir string
 day string
 file *os.File
 bytes int64
}

const maxDiagnosticSize int64 = 10 << 20
const maxDiagnosticDays = 14

func newDiagnosticFiles(dataDir string) *diagnosticFiles {
 return &diagnosticFiles{dir:filepath.Join(dataDir,"logs")}
}
func (d *diagnosticFiles) location() string {return d.dir}
func (d *diagnosticFiles) rotateLocked() error {
 today:=time.Now().Format("2006-01-02")
 if d.file!=nil&&d.day==today&&d.bytes<maxDiagnosticSize{return nil}
 if d.file!=nil{_ = d.file.Close();d.file=nil}
 if err:=os.MkdirAll(d.dir,0700);err!=nil{return err}
 filePath:=filepath.Join(d.dir,"bilipdj-"+today+".log")
 info,err:=os.Stat(filePath)
 if err==nil&&info.Size()>=maxDiagnosticSize {
  // Retain the previous full log until the next day, bounded by 14 days.
  filePath=filepath.Join(d.dir,fmt.Sprintf("bilipdj-%s-%d.log",today,time.Now().Unix()))
 }
 f,err:=os.OpenFile(filePath,os.O_WRONLY|os.O_CREATE|os.O_APPEND,0600)
 if err!=nil{return err}
 d.file=f
 d.day=today
 if st,err:=f.Stat();err==nil{d.bytes=st.Size()}else{d.bytes=0}
 // Ignore unrecognized user files; never delete unrelated state/profile files.
 entries,err:=os.ReadDir(d.dir)
 if err==nil {
  cutoff:=time.Now().AddDate(0,0,-maxDiagnosticDays)
  for _,item:=range entries {
   if item.IsDir()||!strings.HasPrefix(item.Name(),"bilipdj-")||!strings.HasSuffix(item.Name(),".log"){continue}
   if st,err:=item.Info();err==nil&&st.ModTime().Before(cutoff){
    _=os.Remove(filepath.Join(d.dir,item.Name()))
   }
  }
 }
 return nil
}
func (d *diagnosticFiles) line(level,category,message string) error {
 if d==nil{return nil}
 message=sanitizeConsoleLog(message)
 if message==""{return nil}
 d.mu.Lock()
 defer d.mu.Unlock()
 if err:=d.rotateLocked();err!=nil{return err}
 line:=fmt.Sprintf("%s [%s] [%s] %s\n",time.Now().Format(time.RFC3339Nano),level,category,message)
 n,err:=io.WriteString(d.file,line)
 d.bytes+=int64(n)
 return err
}
func (d *diagnosticFiles) Write(data []byte)(int,error){
 // Used by the Go standard log package and collector child process pipes.
 // Do not persist unrestricted multiline messages or API payload bodies.
 for _,row:=range strings.Split(string(data),"\n"){
  if strings.TrimSpace(row)!=""{
   if err:=d.line("INFO","process",row);err!=nil{return 0,err}
  }
 }
 return len(data),nil
}
func (d *diagnosticFiles) Close() error {
 if d==nil{return nil}
 d.mu.Lock();defer d.mu.Unlock()
 if d.file!=nil{err:=d.file.Close();d.file=nil;return err}
 return nil
}

func (a *App) DiagnosticOutput() io.Writer {return a.diagnostic}
func (a *App) DiagnosticDirectory() string {
 if a.diagnostic==nil{return ""}
 return a.diagnostic.location()
}
