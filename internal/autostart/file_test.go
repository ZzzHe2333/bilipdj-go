//go:build linux || darwin

package autostart

import (
    "os"
    "path/filepath"
    "testing"
)
func TestFileAutostartIsOptInAndSafe(t *testing.T){
    path:=filepath.Join(t.TempDir(),"autostart","bilipdj-go.desktop")
    text:="Name=BiliPDJ Go\nExec=example\n"
    on,err:=fileEnabled(path,text)
    if err!=nil||on {t.Fatal("default must be disabled")}
    if _,err=os.Stat(path);!os.IsNotExist(err){t.Fatal("status unexpectedly created file")}
    if err=fileSet(path,text,"Name=BiliPDJ Go",true);err!=nil{t.Fatal(err)}
    if on,err=fileEnabled(path,text);err!=nil||!on{t.Fatal("enable failed",err)}
    if err=fileSet(path,text,"Name=BiliPDJ Go",true);err!=nil{t.Fatal("enable not idempotent",err)}
    if err=fileSet(path,text,"Name=BiliPDJ Go",false);err!=nil{t.Fatal(err)}
    if on,err=fileEnabled(path,text);err!=nil||on{t.Fatal("disable failed")}
    if err=fileSet(path,text,"Name=BiliPDJ Go",false);err!=nil{t.Fatal("disable not idempotent",err)}
    if err=fileSet(path,"custom","other",true);err!=nil{t.Fatal(err)}
    if err=fileSet(path,text,"Name=BiliPDJ Go",false);err==nil{t.Fatal("must not remove unrelated startup entries")}
}
