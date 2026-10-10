//go:build linux

package autostart

import (
    "errors"
    "os"
    "path/filepath"
    "strings"
)
func linuxStartupPath() (string, error) {
    base := os.Getenv("XDG_CONFIG_HOME")
    if base == "" {
        home, err := os.UserHomeDir()
        if err != nil { return "", err }
        base = filepath.Join(home, ".config")
    }
    if !filepath.IsAbs(base) { return "", errors.New("XDG_CONFIG_HOME 必须为绝对路径") }
    return filepath.Join(base, "autostart", "bilipdj-go.desktop"), nil
}
func desktopExec(path string) (string, error) {
    if strings.ContainsAny(path, "\r\n") { return "", errors.New("程序路径含换行符") }
    escape := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`")
    return "\"" + escape.Replace(path) + "\"", nil
}
func linuxEntry() (string, string, error) {
    path, err := linuxStartupPath()
    if err != nil { return "", "", err }
    exe, err := os.Executable()
    if err != nil { return "", "", err }
    command, err := desktopExec(exe)
    if err != nil { return "", "", err }
    return path, "[Desktop Entry]\nType=Application\nName=BiliPDJ Go\nComment=Live queue manager\nExec=" + command + "\nTerminal=false\nX-GNOME-Autostart-enabled=true\n", nil
}
func platformEnabled() (bool, error) {
    path, text, err := linuxEntry()
    if err != nil { return false, err }
    return fileEnabled(path, text)
}
func platformSet(on bool) error {
    path, text, err := linuxEntry()
    if err != nil { return err }
    return fileSet(path, text, "Name=BiliPDJ Go", on)
}
