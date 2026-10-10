//go:build !linux && !darwin && !windows

package perf

import "errors"

func readProcess()(c counters,err error){return c,errors.New("此平台未实现进程监测")}
