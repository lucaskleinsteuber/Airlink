//go:build linux

package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealUpdateHandoffSavesBeforeParentExitAndPreservesOnRejectedReadiness(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("The real update boundary is tested as an ordinary user in Linux CI")
	}
	// This is a harmless command-line fixture, not the AirLink desktop app.
	payload, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	copy(payload[8:11], []byte{'A', 'I', 2})
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved-parent-exit-and-relaunch", true: "rejected-readiness-keeps-original"}[reject], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "AirLink.AppImage")
			previous := append(bytes.Clone(payload), []byte("previous fixture")...)
			if err := os.WriteFile(target, previous, 0700); err != nil {
				t.Fatal(err)
			}
			pub, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(payload)
			m := Manifest{SchemaVersion: 1, Channel: "beta", Platform: "linux", Architecture: "x64", Version: "0.12.0-beta.2"}
			for _, format := range []struct{ kind, ext string }{{"appimage", ".AppImage"}, {"deb", ".deb"}, {"rpm", ".rpm"}, {"arch", ".pkg.tar.zst"}} {
				name := "AirLink-" + m.Version + "-linux-x64" + format.ext
				m.Packages = append(m.Packages, Artifact{Type: format.kind, File: name, URL: "https://github.com/lucaskleinsteuber/Airlink/releases/download/v" + m.Version + "/" + name, Length: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload))})
			}
			body, _ := json.Marshal(m)
			packagePath := filepath.Join(dir, m.Packages[0].File)
			feed := filepath.Join(dir, "feed.json")
			signature := filepath.Join(dir, "feed.signature")
			for path, data := range map[string][]byte{packagePath: payload, feed: body, signature: []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, body)))} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			parent := exec.Command("/usr/bin/sleep", "30")
			if err := parent.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Process.Kill(); _ = parent.Wait() }()
			saved := false
			failure := errors.New("service still running")
			o := ApplyOptions{ParentPID: parent.Process.Pid, ParentExecutable: "/usr/bin/sleep", Target: target, Package: packagePath, Feed: feed, SignatureFile: signature, InstalledVersion: "0.12.0-beta.1", PublicKey: pub, Ready: func() error {
				saved = true
				current, _ := os.ReadFile(target)
				if !bytes.Equal(current, previous) {
					t.Error("target changed before readiness")
				}
				if reject {
					return failure
				}
				_ = parent.Process.Kill()
				return parent.Wait()
			}}
			// Wait returns ExitError for the intentional simulated parent shutdown.
			originalReady := o.Ready
			o.Ready = func() error {
				e := originalReady()
				var exited *exec.ExitError
				if errors.As(e, &exited) {
					return nil
				}
				return e
			}
			err = ApplyLinux(o)
			if !saved {
				t.Fatal("readiness handshake was bypassed", err)
			}
			current, _ := os.ReadFile(target)
			if reject {
				if !errors.Is(err, failure) || !bytes.Equal(current, previous) {
					t.Fatal("rejected handoff changed the app", err)
				}
			} else if err != nil || !bytes.Equal(current, payload) {
				t.Fatal("verified handoff did not install and launch the CLI fixture", err)
			}
		})
	}
}
