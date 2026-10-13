//go:build windows

package main

import (
    "reflect"
    "testing"
)

func TestTrayIconRemovalImmediatelyAndOnlyOnce(t *testing.T) {
    tray:=&trayControl{iconAdded:true}
    calls:=0
    del:=func(icon *notifyIcon) bool{
        if icon!=&tray.icon{t.Fatal("wrong notification icon")}
        calls++
        return true
    }
    tray.removeIconWith(del)
    if tray.iconAdded{t.Fatal("NIM_DELETE success did not mark icon removed")}
    tray.removeIconWith(del) // WM_DESTROY must not delete the icon twice.
    if calls!=1{t.Fatalf("expected one NIM_DELETE call, got %d",calls)}
}

func TestTrayIconDeletionFailureRetriesOnWindowDestroy(t *testing.T) {
    tray:=&trayControl{iconAdded:true}
    calls:=0
    tray.removeIconWith(func(*notifyIcon) bool{
        calls++
        return false
    })
    if !tray.iconAdded{t.Fatal("failed Shell_NotifyIcon delete must allow a retry")}
    tray.removeIconWith(func(*notifyIcon) bool{
        calls++
        return true
    })
    if tray.iconAdded||calls!=2{t.Fatalf("WM_DESTROY retry failed, attempts=%d, iconAdded=%t",calls,tray.iconAdded)}
}

func TestTrayIconIsRemovedBeforeWindowAndShutdown(t *testing.T) {
    order:=[]string{}
    beginTrayExit(
        func(){order=append(order,"remove icon")},
        func(){order=append(order,"close tray window")},
        func(){order=append(order,"request server shutdown")},
    )
    want:=[]string{"remove icon","close tray window","request server shutdown"}
    if !reflect.DeepEqual(order,want){
        t.Fatalf("tray Exit must remove icon first: got %v want %v",order,want)
    }
}

func TestNoTrayIconDoesNotIssueDeletion(t *testing.T){
    tray:=&trayControl{}
    tray.removeIconWith(func(*notifyIcon)bool{
        t.Fatal("NIM_DELETE called for an icon that was not installed")
        return true
    })
    var nilTray *trayControl
    nilTray.removeIconWith(func(*notifyIcon)bool{
        t.Fatal("NIM_DELETE called with nil state")
        return true
    })
}
