package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Web/OBS styles belong to PR #312 *-web.json. Tk *-win.json is not read or
// written by the Go server. Legacy style.json may still be imported manually.
func (a *App) webAppearancePath(kind string) string {
	if a.storagePlan.Mode != "managed" {
		return ""
	} // Docker explicit paths stay unchanged.
	name := "style-web.json"
	if kind == "appearance" {
		name = "appearance-web.json"
	}
	return filepath.Join(filepath.Dir(a.dataPath), name)
}
func (a *App) loadWebAppearanceFiles() {
	for _, kind := range []string{"style", "appearance"} {
		path := a.webAppearancePath(kind)
		if path == "" {
			continue
		}
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			continue
		}
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil || len(v) == 0 {
			continue
		}
		if kind == "style" {
			a.style = v
		} else {
			a.appearance = v
		}
	}
}
func (a *App) writeWebAppearanceFile(kind string, raw []byte) error {
	path := a.webAppearancePath(kind)
	if path == "" {
		return nil
	}
	if len(raw) > 65536 {
		return fmt.Errorf("web appearance data exceeds limit")
	}
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe appearance target")
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".web-style-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
