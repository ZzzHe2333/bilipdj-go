package core

import "testing"

// Existing style-web.json files may contain the old dark-panel defaults.
// The new flag takes precedence in the OBS renderer, while the original
// show_background setting is still available for an explicit opaque mode.
func TestDefaultOBSStyleIsTrulyTransparent(t *testing.T) {
 style := defaultStyle()
 if style["transparent_background"] != true {
  t.Fatalf("default OBS background must be fully transparent, got %#v", style["transparent_background"])
 }
 if style["show_background"] != false {
  t.Fatalf("default dark board must be disabled, got %#v", style["show_background"])
 }
 if style["queue_font_size"] == nil {
  t.Fatal("OBS font size should remain adjustable")
 }
}
