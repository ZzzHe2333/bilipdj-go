//go:build windows

package update

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008}
}
func waitProcess(pid int, deadline time.Duration) error {
	p, e := os.FindProcess(pid)
	if e != nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { _, err := p.Wait(); done <- err }()
	select {
	case <-done:
		return nil
	case <-time.After(deadline):
		return errors.New("等待旧进程退出超时")
	}
}
