//go:build !windows

package main

func desktopStart(_ string, _ func()) func() { return func() {} }
func desktopError(_ string)                  {}
func desktopDataDir() string                 { return "data" }
