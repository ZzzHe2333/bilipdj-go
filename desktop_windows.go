//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// The Windows release uses -H=windowsgui: no console window is created.
// A message-only hidden window owns the notification-area icon. All Win32
// calls live here, keeping the Linux/macOS/Docker binaries dependency-free.
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClass    = user32.NewProc("RegisterClassW")
	procCreateWindow     = user32.NewProc("CreateWindowExW")
	procDefWindow        = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuit         = user32.NewProc("PostQuitMessage")
	procPostMessage      = user32.NewProc("PostMessageW")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procGetCursor        = user32.NewProc("GetCursorPos")
	procCreateMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu       = user32.NewProc("AppendMenuW")
	procTrackPopup       = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procLoadIcon         = user32.NewProc("LoadIconW")
	procMessageBox       = user32.NewProc("MessageBoxW")
	procShellNotify      = shell32.NewProc("Shell_NotifyIconW")
	procShellExecute     = shell32.NewProc("ShellExecuteW")
	procModuleHandle     = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmTrayIcon      = 0x8001 // WM_APP + 1
	nimAdd          = 0
	nimDelete       = 2
	nifMessage      = 1
	nifIcon         = 2
	nifTip          = 4
	menuOpen        = 1001
	menuQuit        = 1002
)

type winClass struct {
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
}
type winPoint struct{ X, Y int32 }
type winMessage struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             winPoint
	Private        uint32
}
type notifyIcon struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

type trayControl struct {
	url  string
	quit func()
	icon notifyIcon
}

var trayState *trayControl // Single-instance tray callback; owned by process lifetime.

func wide(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func openDesktopURL(url string) {
	// ShellExecute invokes the user's default browser, without a command shell.
	verb := wide("open")
	target := wide(url)
	code, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(target)
	if code <= 32 {
		log.Printf("cannot launch browser: Windows ShellExecute code=%d", code)
	}
}
func desktopError(text string) {
	message := wide(text)
	title := wide("BiliPDJ Go")
	procMessageBox.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
	runtime.KeepAlive(message)
	runtime.KeepAlive(title)
}

func trayWindowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	state := trayState
	switch msg {
	case wmTrayIcon:
		if state != nil {
			switch uint32(lparam) {
			case wmLButtonDblClk:
				openDesktopURL(state.url)
			case wmRButtonUp:
				menu, _, _ := procCreateMenu.Call()
				if menu == 0 {
					return 0
				}
				defer procDestroyMenu.Call(menu)
				open := wide("打开 Web 管理界面")
				exit := wide("退出 BiliPDJ Go")
				procAppendMenu.Call(menu, 0, menuOpen, uintptr(unsafe.Pointer(open)))
				procAppendMenu.Call(menu, 0x800, 0, 0) // separator
				procAppendMenu.Call(menu, 0, menuQuit, uintptr(unsafe.Pointer(exit)))
				runtime.KeepAlive(open)
				runtime.KeepAlive(exit)
				var point winPoint
				procGetCursor.Call(uintptr(unsafe.Pointer(&point)))
				procSetForeground.Call(hwnd)
				action, _, _ := procTrackPopup.Call(menu, 0x100|0x2, uintptr(point.X), uintptr(point.Y), 0, hwnd, 0)
				switch action {
				case menuOpen:
					openDesktopURL(state.url)
				case menuQuit:
					state.quit()
				}
				procPostMessage.Call(hwnd, 0, 0, 0)
			}
		}
		return 0
	case wmCommand:
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if state != nil {
			procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&state.icon)))
		}
		procPostQuit.Call(0)
		return 0
	}
	result, _, _ := procDefWindow.Call(hwnd, uintptr(msg), wparam, lparam)
	return result
}

func runTray(url string, quit func(), ready chan<- uintptr) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className := wide("BiliPDJGoTrayWindow")
	instance, _, _ := procModuleHandle.Call(0)
	class := winClass{WndProc: syscall.NewCallback(trayWindowProc), Instance: instance, ClassName: className}
	atom, _, _ := procRegisterClass.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		ready <- 0
		return
	}
	title := wide("BiliPDJ Go")
	hwnd, _, _ := procCreateWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		ready <- 0
		return
	}
	state := &trayControl{url: url, quit: quit}
	state.icon.Hwnd = hwnd
	state.icon.ID = 1
	state.icon.Size = uint32(unsafe.Sizeof(state.icon))
	state.icon.Flags = nifMessage | nifIcon | nifTip
	state.icon.CallbackMessage = wmTrayIcon
	icon, _, _ := procLoadIcon.Call(0, 32512) // IDI_APPLICATION
	state.icon.Icon = icon
	copy(state.icon.Tip[:], syscall.StringToUTF16("BiliPDJ Go · 双击打开管理界面"))
	trayState = state
	ok, _, _ := procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&state.icon)))
	if ok == 0 {
		trayState = nil
		procDestroyWindow.Call(hwnd)
		ready <- 0
		return
	}
	ready <- hwnd
	var msg winMessage
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	trayState = nil
}
func desktopStart(url string, quit func()) func() {
	ready := make(chan uintptr, 1)
	go runTray(url, quit, ready)
	hwnd := <-ready
	if hwnd == 0 {
		log.Print("system tray setup failed; web control panel remains available")
	}
	openDesktopURL(url)
	return func() {
		if hwnd != 0 {
			procPostMessage.Call(hwnd, wmClose, 0, 0)
		}
	}
}

// Explorer shortcuts can start in arbitrary directories; keep new installs
// beside the executable, while respecting an existing old ./data/state.json.
func desktopDataDir() string {
	if _, err := os.Stat(filepath.Join("data", "state.json")); err == nil {
		return "data"
	}
	path, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(path), "data")
	}
	return "data"
}
