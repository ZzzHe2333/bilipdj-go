//go:build !windows

package update

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func setDetached(cmd *exec.Cmd) {}
func waitProcess(pid int, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("等待旧进程退出超时")
}
