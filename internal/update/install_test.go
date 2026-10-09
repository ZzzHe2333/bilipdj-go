package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeInstallZIP(t *testing.T, data []byte, symlink bool) Downloaded {
	t.Helper()
	dir := t.TempDir()
	name := "bilipdj-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetMode(0755)
	if symlink {
		hdr.SetMode(os.ModeSymlink | 0777)
	}
	entry, e := w.CreateHeader(hdr)
	if e != nil {
		t.Fatal(e)
	}
	entry.Write(data)
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	zipPath := filepath.Join(dir, "0.10.0-bilipdj-go-"+runtime.GOOS+"-"+runtime.GOARCH+".zip")
	if e = os.WriteFile(zipPath, buf.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(buf.Bytes())
	return Downloaded{File: zipPath, Version: "v0.10.0", SHA256: hex.EncodeToString(sum[:])}
}
func TestPrepareInstallChecksAndNeverOverwritesUserData(t *testing.T) {
	d := makeInstallZIP(t, []byte("hello new exe"), false)
	current := filepath.Join(t.TempDir(), "current")
	if e := os.WriteFile(current, []byte("original program"), 0700); e != nil {
		t.Fatal(e)
	}
	helper, job, e := PrepareInstall(d, current, []string{"-listen", "127.0.0.1:10001"}, "http://127.0.0.1:10001/health")
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(current)
	if e != nil || string(b) != "original program" {
		t.Fatalf("original replaced during prepare: %q %v", b, e)
	}
	if _, e = os.Stat(helper); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(job); e != nil {
		t.Fatal(e)
	}
	d.SHA256 = strings.Repeat("f", 64)
	if _, _, e = PrepareInstall(d, current, nil); e == nil || !strings.Contains(e.Error(), "ZIP") {
		t.Fatalf("tampered digest accepted: %v", e)
	}
}
func TestPrepareInstallRejectsSymlinkZipEntry(t *testing.T) {
	d := makeInstallZIP(t, []byte("fake symlink"), true)
	current := filepath.Join(t.TempDir(), "current")
	os.WriteFile(current, []byte("old"), 0700)
	if _, _, e := PrepareInstall(d, current, nil); e == nil {
		t.Fatal("symlink entry accepted")
	}
}
func TestSemverNoDowngrade(t *testing.T) {
	if newer("v0.9.0", "0.10.0") || !newer("v0.10.0", "0.9.0") || newer("v0.10.0", "v0.10.0") {
		t.Fatal("semver comparison incorrect")
	}
}
