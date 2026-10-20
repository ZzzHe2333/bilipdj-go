//go:build !windows

package core

import "os/exec"

func setDouyinNodeHidden(cmd *exec.Cmd) {}
