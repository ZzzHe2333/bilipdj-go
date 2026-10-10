//go:build darwin

package autostart

import (
    "bytes"
    "encoding/xml"
    "os"
    "path/filepath"
)
func macEntry() (string, string, error) {
    home, err := os.UserHomeDir()
    if err != nil { return "", "", err }
    exe, err := os.Executable()
    if err != nil { return "", "", err }
    var escaped bytes.Buffer
    if err := xml.EscapeText(&escaped, []byte(exe)); err != nil { return "", "", err }
    path := filepath.Join(home, "Library", "LaunchAgents", "com.zzzhe2333.bilipdj-go.plist")
    doc := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.zzzhe2333.bilipdj-go</string>
<key>ProgramArguments</key><array><string>` + escaped.String() + `</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><false/>
</dict></plist>
`
    return path, doc, nil
}
func platformEnabled() (bool, error) {
    path, text, err := macEntry()
    if err != nil { return false, err }
    return fileEnabled(path, text)
}
func platformSet(on bool) error {
    path, text, err := macEntry()
    if err != nil { return err }
    return fileSet(path, text, "com.zzzhe2333.bilipdj-go", on)
}
