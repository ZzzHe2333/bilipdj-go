//go:build windows

package autostart

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "syscall"
    "unsafe"
)

const (
    runPath = `Software\Microsoft\Windows\CurrentVersion\Run`
    runName = "BiliPDJ-Go"
    currentUser = uintptr(0x80000001)
    keyRead = 0x20019
    keySetValue = 0x0002
    regSZ = 1
)
var (
    advapi = syscall.NewLazyDLL("advapi32.dll")
    regOpen = advapi.NewProc("RegOpenKeyExW")
    regCreate = advapi.NewProc("RegCreateKeyExW")
    regQuery = advapi.NewProc("RegQueryValueExW")
    regSet = advapi.NewProc("RegSetValueExW")
    regDelete = advapi.NewProc("RegDeleteValueW")
    regClose = advapi.NewProc("RegCloseKey")
)
func startupCommand() (string,error) {
    exe, err := os.Executable()
    if err != nil { return "",err }
    if strings.ContainsRune(exe,'"') { return "",fmt.Errorf("无效的程序路径") }
    return "\"" + filepath.Clean(exe) + "\"",nil
}
func registryRunKey(create bool) (uintptr,error) {
    path := syscall.StringToUTF16Ptr(runPath)
    var handle uintptr
    var result uintptr
    if create {
        result,_,_ = regCreate.Call(currentUser,uintptr(unsafe.Pointer(path)),0,0,0,keyRead|keySetValue,0,uintptr(unsafe.Pointer(&handle)),0)
    } else {
        result,_,_ = regOpen.Call(currentUser,uintptr(unsafe.Pointer(path)),0,keyRead|keySetValue,uintptr(unsafe.Pointer(&handle)))
    }
    runtime.KeepAlive(path)
    if result!=0 { return 0,syscall.Errno(result) }
    return handle,nil
}
func registryRead(key uintptr) (string,bool,error) {
    name:=syscall.StringToUTF16Ptr(runName)
    var typ,length uint32
    result,_,_:=regQuery.Call(key,uintptr(unsafe.Pointer(name)),0,uintptr(unsafe.Pointer(&typ)),0,uintptr(unsafe.Pointer(&length)))
    if result==uintptr(syscall.ERROR_FILE_NOT_FOUND) { return "",false,nil }
    if result!=0 { return "",false,syscall.Errno(result) }
    if typ!=regSZ || length==0 || length>32768 { return "",true,fmt.Errorf("启动项类型或长度无效") }
    buf:=make([]uint16,(int(length)+1)/2)
    result,_,_=regQuery.Call(key,uintptr(unsafe.Pointer(name)),0,uintptr(unsafe.Pointer(&typ)),uintptr(unsafe.Pointer(&buf[0])),uintptr(unsafe.Pointer(&length)))
    runtime.KeepAlive(name)
    runtime.KeepAlive(buf)
    if result!=0 { return "",false,syscall.Errno(result) }
    return syscall.UTF16ToString(buf),true,nil
}
func platformEnabled() (bool,error) {
    key,err:=registryRunKey(false)
    if err==syscall.ERROR_FILE_NOT_FOUND { return false,nil }
    if err!=nil { return false,err }
    defer regClose.Call(key)
    value,exists,err:=registryRead(key)
    if err!=nil { return false,err }
    expected,err:=startupCommand()
    if err!=nil { return false,err }
    return exists&&strings.EqualFold(value,expected),nil
}
func platformSet(on bool) error {
    if !on {
        key,err:=registryRunKey(false)
        if err==syscall.ERROR_FILE_NOT_FOUND { return nil }
        if err!=nil { return err }
        defer regClose.Call(key)
        name:=syscall.StringToUTF16Ptr(runName)
        result,_,_:=regDelete.Call(key,uintptr(unsafe.Pointer(name)))
        runtime.KeepAlive(name)
        if result==uintptr(syscall.ERROR_FILE_NOT_FOUND) { return nil }
        if result!=0 { return syscall.Errno(result) }
        return nil
    }
    command,err:=startupCommand()
    if err!=nil { return err }
    key,err:=registryRunKey(true)
    if err!=nil { return err }
    defer regClose.Call(key)
    name:=syscall.StringToUTF16Ptr(runName)
    value:=syscall.StringToUTF16(command)
    result,_,_:=regSet.Call(key,uintptr(unsafe.Pointer(name)),0,regSZ,uintptr(unsafe.Pointer(&value[0])),uintptr(len(value)*2))
    runtime.KeepAlive(name)
    runtime.KeepAlive(value)
    if result!=0 { return syscall.Errno(result) }
    return nil
}
