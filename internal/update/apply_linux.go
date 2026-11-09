//go:build linux

package update

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func ownRegular(path string) (os.FileInfo, error) {
	f, e := os.Lstat(path)
	if e != nil || !f.Mode().IsRegular() {
		return nil, errors.New("update path is not a regular file")
	}
	stat, ok := f.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return nil, errors.New("update file is not owned by the current user")
	}
	return f, nil
}
func imageHeader(path string) error {
	f, e := OpenRegular(path, PackageLimit)
	if e != nil {
		return e
	}
	defer f.Close()
	header := make([]byte, 16)
	if _, e = io.ReadFull(f, header); e != nil {
		return e
	}
	if !bytes.Equal(header[:4], []byte{0x7f, 'E', 'L', 'F'}) || header[4] != 2 || !bytes.Equal(header[8:11], []byte{'A', 'I', 2}) {
		return errors.New("update target is not a 64-bit type 2 AppImage")
	}
	return nil
}
func ApplyLinux(o ApplyOptions) error {
	if runtime.GOARCH != "amd64" || os.Geteuid() == 0 {
		return errors.New("AppImage updates require an ordinary Linux x64 user")
	}
	body, e := ReadRegular(o.Feed, 1<<20)
	if e != nil {
		return e
	}
	encoded, e := ReadRegular(o.SignatureFile, 4096)
	if e != nil {
		return e
	}
	signature, e := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
	if e != nil {
		return e
	}
	manifest, e := ParseManifest(body, signature, o.PublicKey)
	if e != nil {
		return e
	}
	comparison, e := CompareVersions(manifest.Version, o.InstalledVersion)
	if e != nil || comparison <= 0 {
		return errors.New("update must be newer than the installed application")
	}
	var artifact Artifact
	for _, a := range manifest.Packages {
		if a.Type == "appimage" {
			artifact = a
		}
	}
	if e = VerifyArtifact(o.Package, artifact, o.PublicKey); e != nil {
		return e
	}
	if e = imageHeader(o.Package); e != nil {
		return e
	}
	old, e := ownRegular(o.Target)
	if e != nil {
		return e
	}
	if e = imageHeader(o.Target); e != nil {
		return e
	}
	parentDir, e := os.Stat(filepath.Dir(o.Target))
	if e != nil || !parentDir.IsDir() || parentDir.Mode().Perm()&0022 != 0 {
		return errors.New("AppImage directory must be writable only by its owner")
	}
	if o.ParentPID <= 1 || o.ParentPID == os.Getpid() {
		return errors.New("invalid parent process")
	}
	proc := filepath.Join("/proc", itoa(o.ParentPID))
	procInfo, e := os.Stat(proc)
	if e != nil {
		return errors.New("the application process is no longer available")
	}
	procStat, ok := procInfo.Sys().(*syscall.Stat_t)
	if !ok || int(procStat.Uid) != os.Geteuid() {
		return errors.New("the application process is owned by another user")
	}
	expected, e := os.Stat(o.ParentExecutable)
	if e != nil {
		return e
	}
	actual, e := os.Stat(filepath.Join(proc, "exe"))
	if e != nil || !os.SameFile(expected, actual) {
		return errors.New("application executable differs from the update request")
	}
	identity, e := os.ReadFile(filepath.Join(proc, "stat"))
	if e != nil {
		return e
	}
	start := processStart(identity)
	if start == "" {
		return errors.New("application process identity unavailable")
	}
	if o.Ready != nil {
		if e = o.Ready(); e != nil {
			return e
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(proc, "stat"))
		if os.IsNotExist(err) || err == nil && (processStart(b) != start || processExited(b)) {
			break
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("application did not exit; update was not installed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	launch := func() error {
		cmd := exec.Command(o.Target)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	if e = replaceImage(o, artifact, old); e != nil {
		_ = launch()
		return e
	}
	return launch()
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(b[i:])
}
func processStart(stat []byte) string {
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return ""
	}
	fields := strings.Fields(string(stat[i+1:]))
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}
func processExited(stat []byte) bool {
	i := strings.LastIndexByte(string(stat), ')')
	if i < 0 {
		return false
	}
	fields := strings.Fields(string(stat[i+1:]))
	return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
}

func replaceImage(o ApplyOptions, a Artifact, old os.FileInfo) error {
	current, e := ownRegular(o.Target)
	if e != nil || !os.SameFile(old, current) {
		return errors.New("AppImage changed while preparing the update")
	}
	in, e := OpenRegular(o.Package, PackageLimit)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(o.Target), ".airlink-update-*")
	if e != nil {
		return e
	}
	name := out.Name()
	defer os.Remove(name)
	if _, e = io.CopyBuffer(out, in, make([]byte, 128<<10)); e == nil {
		e = out.Chmod(0700)
	}
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	// Verify the exact staged bytes, not a previously opened source path.
	if e = VerifyArtifact(name, a, o.PublicKey); e != nil {
		return e
	}
	if e = os.Rename(name, o.Target); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(o.Target))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
