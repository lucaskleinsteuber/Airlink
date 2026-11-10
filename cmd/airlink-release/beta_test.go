package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBetaSignsExactPlatformSetAndRejectsTampering(t *testing.T) {
	for _, target := range []string{"none", "manifest", "manifest-signature", "linux-feed", "linux-signature", "macos-arm64", "windows-arm64", "package", "sums"} {
		t.Run(target, func(t *testing.T) {
			o := fixture(t)
			o.Version = "0.12.0-beta.1"
			o.Build = "14"
			for _, name := range betaPackageNames(o.Version) {
				if err := os.WriteFile(filepath.Join(o.Dist, name), []byte("Synthetic beta payload: "+name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := signRelease(o); err != nil {
				t.Fatal(err)
			}
			if err := verifyRelease(o); err != nil {
				t.Fatal(err)
			}
			if target == "none" {
				return
			}
			path := map[string]string{"manifest": filepath.Join(o.Dist, manifestName), "manifest-signature": filepath.Join(o.Dist, manifestName+".signature"), "linux-feed": filepath.Join(o.Updates, "linux-x64.json"), "linux-signature": filepath.Join(o.Updates, "linux-x64.json.signature"), "macos-arm64": filepath.Join(o.Updates, "macos-arm64.xml"), "windows-arm64": filepath.Join(o.Updates, "windows-arm64.xml"), "package": filepath.Join(o.Dist, betaPackageNames(o.Version)[6]), "sums": filepath.Join(o.Dist, "SHA256SUMS.txt")}[target]
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteString("tampered")
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err = verifyRelease(o); err == nil {
				t.Fatal("tampered release was accepted", target)
			}
		})
	}
}
func TestBetaMissingPackageDoesNotPublishFeeds(t *testing.T) {
	o := fixture(t)
	o.Version = "0.12.0-beta.1"
	o.Build = "14"
	for _, name := range betaPackageNames(o.Version)[:7] {
		if err := os.WriteFile(filepath.Join(o.Dist, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := signRelease(o); err == nil {
		t.Fatal("missing Arch package accepted")
	}
	if _, err := os.Stat(filepath.Join(o.Updates, "macos-arm64.xml")); !os.IsNotExist(err) {
		t.Fatal("partial feed publication", err)
	}
}
