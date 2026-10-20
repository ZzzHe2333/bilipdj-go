//go:build windows

package core

import (
 "os/exec"
 "syscall"
)

func setDouyinNodeHidden(cmd *exec.Cmd) {
 cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow:true}
}
