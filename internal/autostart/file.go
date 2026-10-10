//go:build linux || darwin

package autostart

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
)

func fileEnabled(path, expected string) (bool, error) {
    data, err := os.ReadFile(path)
    if errors.Is(err, os.ErrNotExist) { return false, nil }
    if err != nil { return false, err }
    return string(data) == expected, nil
}
func fileSet(path, expected, marker string, on bool) error {
    data, err := os.ReadFile(path)
    if err != nil && !errors.Is(err, os.ErrNotExist) { return err }
    if err == nil && !strings.Contains(string(data), marker) {
        return errors.New("该登录启动项已被其他程序占用，拒绝覆盖")
    }
    if !on {
        if errors.Is(err, os.ErrNotExist) { return nil }
        return os.Remove(path)
    }
    if string(data) == expected { return nil }
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { return err }
    tmp, err := os.CreateTemp(filepath.Dir(path), ".bilipdj-startup-*")
    if err != nil { return err }
    defer os.Remove(tmp.Name())
    defer tmp.Close()
    if _, err := tmp.WriteString(expected); err != nil { return err }
    if err := tmp.Chmod(0600); err != nil { return err }
    if err := tmp.Close(); err != nil { return err }
    return os.Rename(tmp.Name(), path)
}
