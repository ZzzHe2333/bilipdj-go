//go:build !windows && !linux && !darwin
package autostart
func platformEnabled() (bool,error) { return false,ErrUnsupported }
func platformSet(bool) error { return ErrUnsupported }
