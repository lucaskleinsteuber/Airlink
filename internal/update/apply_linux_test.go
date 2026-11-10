//go:build linux

package update

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAppImageReplacementVerifiesStagedBytesAndKeepsOnlyCurrent(t *testing.T) {
	m, _, _, pub, payload := signedFixture(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "AirLink.AppImage")
	source := filepath.Join(dir, "download.AppImage")
	old := bytes.Clone(payload)
	copy(old[256:], []byte("previous version"))
	if e := os.WriteFile(target, old, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(source, payload, 0600); e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(target)
	if e != nil {
		t.Fatal(e)
	}
	var a Artifact
	for _, v := range m.Packages {
		if v.Type == "appimage" {
			a = v
		}
	}
	o := ApplyOptions{Target: target, Package: source, PublicKey: pub}
	if e = replaceImage(o, a, info); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(target)
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("replacement mismatch", e)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".airlink-update-*"))
	if len(matches) != 0 {
		t.Fatal("left staged app copies", matches)
	}
	info, _ = os.Lstat(target)
	verified := bytes.Clone(payload)
	payload[512] ^= 1
	_ = os.WriteFile(source, payload, 0600)
	if e = replaceImage(o, a, info); e == nil {
		t.Fatal("corrupt staging was published")
	}
	got, _ = os.ReadFile(target)
	if !bytes.Equal(got, verified) {
		t.Fatal("corrupt package replaced the app")
	}
}
func TestAppImageTargetAndProcessBoundaries(t *testing.T) {
	if !processExited([]byte("123 (airlink name) Z 1 2 3")) {
		t.Fatal("zombie parent must count as exited")
	}
	if processStart([]byte("malformed")) != "" {
		t.Fatal("malformed process identity accepted")
	}
	file := filepath.Join(t.TempDir(), "not-image")
	_ = os.WriteFile(file, []byte("not an app"), 0600)
	if e := imageHeader(file); e == nil {
		t.Fatal("non-AppImage target accepted")
	}
	if e := ApplyLinux(ApplyOptions{ParentPID: 1}); e == nil {
		t.Fatal("invalid update request accepted")
	}
}
