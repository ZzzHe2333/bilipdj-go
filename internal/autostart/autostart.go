// Package autostart manages per-user login startup. Opt-in only.
package autostart

import (
    "errors"
    "os"
    "runtime"
)

var ErrUnsupported = errors.New("当前环境不支持开机自启，请使用系统服务或容器重启策略")

type State struct {
    Supported bool   `json:"supported"`
    Enabled   bool   `json:"enabled"`
    Platform  string `json:"platform"`
    Note      string `json:"note"`
}
type Controller interface {
    Status() (State, error)
    Set(bool) error
}
type Service struct{}
func New() *Service { return &Service{} }

func supported() bool {
    if runtime.GOOS == "linux" {
        if _, err := os.Stat("/.dockerenv"); err == nil { return false }
        if os.Getenv("KUBERNETES_SERVICE_HOST") != "" { return false }
    }
    return runtime.GOOS == "windows" || runtime.GOOS == "linux" || runtime.GOOS == "darwin"
}
func (*Service) Status() (State, error) {
    s := State{Supported: supported(), Platform: runtime.GOOS}
    if !s.Supported {
        s.Note = "该环境请使用系统服务或 Docker/容器重启策略"
        return s, nil
    }
    on, err := platformEnabled()
    if err != nil { return s, err }
    s.Enabled = on
    s.Note = "仅当前用户登录时自动启动；默认关闭"
    return s, nil
}
func (*Service) Set(on bool) error {
    if !supported() { return ErrUnsupported }
    return platformSet(on)
}
