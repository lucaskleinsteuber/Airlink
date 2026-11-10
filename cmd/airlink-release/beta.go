package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/update"
)

func modernRelease(version string) bool {
	parts, err := update.VersionParts(version)
	return err == nil && (parts[0] > 0 || parts[1] >= 12)
}
func windowsNumericVersion(o releaseOptions) string {
	if modernRelease(o.Version) {
		return strings.SplitN(o.Version, "-", 2)[0] + "." + o.Build
	}
	return o.Version
}
func betaPackageNames(version string) []string {
	prefix := "AirLink-" + version + "-"
	return []string{prefix + "macos-arm64.dmg", prefix + "macos-x64.dmg", prefix + "windows-x64-setup.exe", prefix + "windows-arm64-setup.exe", prefix + "linux-x64.AppImage", prefix + "linux-x64.deb", prefix + "linux-x64.rpm", prefix + "linux-x64.pkg.tar.zst"}
}

var betaChannels = []string{"macos-arm64", "macos-x64", "windows-x64", "windows-arm64", "linux-x64"}

// The old three-package publication/curation format stays available only for
// historical stable-version fixtures. Beta publication has an exact new set.
func signBetaRelease(o releaseOptions) error {
	if err := validateRelease(o.Version, o.Build); err != nil {
		return err
	}
	if o.Published.IsZero() {
		return errors.New("publication timestamp is required")
	}
	if err := outsideRepository(o.KeyDirectory, o.Repository); err != nil {
		return err
	}
	_, public, err := publicKey(o.PublicConfig)
	if err != nil {
		return err
	}
	private, err := privateSeed(o.KeyDirectory, false)
	if err != nil {
		return err
	}
	if !private.Public().(ed25519.PublicKey).Equal(public) {
		return errors.New("publishing key differs from the public trust anchor")
	}
	m := releaseManifest{SchemaVersion: 2, Version: o.Version, Build: o.Build, Published: o.Published.UTC().Format(time.RFC3339), KeyFingerprint: fingerprint(public)}
	for _, name := range betaPackageNames(o.Version) {
		body, err := readBounded(filepath.Join(o.Dist, name), packageLimit)
		if err != nil {
			return err
		}
		m.Packages = append(m.Packages, record(name, body, private))
	}
	outputs := map[string][]byte{}
	for i, channel := range betaChannels {
		var content []byte
		name := channel + ".xml"
		if channel == "linux-x64" {
			name = channel + ".json"
			linux := update.Manifest{SchemaVersion: 1, Channel: "beta", Platform: "linux", Architecture: "x64", Version: o.Version}
			for j, kind := range []string{"appimage", "deb", "rpm", "arch"} {
				p := m.Packages[j+4]
				linux.Packages = append(linux.Packages, update.Artifact{Type: kind, File: p.File, URL: releaseBase + "v" + o.Version + "/" + p.File, Length: p.Length, SHA256: p.SHA256, Signature: p.Signature})
			}
			content, err = json.MarshalIndent(linux, "", "  ")
			if err != nil {
				return err
			}
			content = append(content, '\n')
		} else {
			content = feedXML(o, channel, m.Packages[i])
		}
		if strings.HasPrefix(channel, "macos") {
			content = signedMacFeed(content, private)
		}
		entry := record(name, content, private)
		m.Feeds = append(m.Feeds, entry)
		outputs[name] = content
		if !strings.HasPrefix(channel, "macos") {
			outputs[name+".signature"] = []byte(entry.Signature + "\n")
		}
	}
	// Validate the complete package set before writing any publication metadata.
	for name, body := range outputs {
		if err = atomicPublicFile(filepath.Join(o.Updates, name), body); err != nil {
			return err
		}
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, body)) + "\n")
	if err = atomicPublicFile(filepath.Join(o.Dist, manifestName), body); err != nil {
		return err
	}
	if err = atomicPublicFile(filepath.Join(o.Dist, manifestName+".signature"), signature); err != nil {
		return err
	}
	if err = atomicPublicFile(filepath.Join(o.Dist, "SHA256SUMS.txt"), betaSums(m, body, signature)); err != nil {
		return err
	}
	return verifyBetaRelease(o)
}
func betaSums(m releaseManifest, body, signature []byte) []byte {
	var lines []string
	for _, p := range m.Packages {
		lines = append(lines, p.SHA256+"  "+p.File)
	}
	for name, payload := range map[string][]byte{manifestName: body, manifestName + ".signature": signature} {
		lines = append(lines, fmt.Sprintf("%x  %s", sha256.Sum256(payload), name))
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}
func verifyBetaRelease(o releaseOptions) error {
	if err := validateRelease(o.Version, o.Build); err != nil {
		return err
	}
	_, public, err := publicKey(o.PublicConfig)
	if err != nil {
		return err
	}
	body, err := readBounded(filepath.Join(o.Dist, manifestName), 1<<20)
	if err != nil {
		return err
	}
	encoded, err := readBounded(filepath.Join(o.Dist, manifestName+".signature"), 4096)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || !ed25519.Verify(public, body, sig) {
		return errors.New("release manifest signature is invalid")
	}
	var m releaseManifest
	if json.Unmarshal(body, &m) != nil || m.SchemaVersion != 2 || m.Version != o.Version || m.Build != o.Build || m.KeyFingerprint != fingerprint(public) || len(m.Packages) != 8 || len(m.Feeds) != 5 {
		return errors.New("beta manifest does not describe the exact release")
	}
	if _, err = time.Parse(time.RFC3339, m.Published); err != nil {
		return errors.New("invalid publication time")
	}
	for i, name := range betaPackageNames(o.Version) {
		entry := m.Packages[i]
		if entry.File != name {
			return errors.New("unexpected package architecture or path")
		}
		data, err := readBounded(filepath.Join(o.Dist, name), packageLimit)
		if err != nil {
			return err
		}
		if err = validateRecord(entry, data, public); err != nil {
			return err
		}
	}
	for i, channel := range betaChannels {
		name := channel + ".xml"
		if channel == "linux-x64" {
			name = channel + ".json"
		}
		entry := m.Feeds[i]
		if entry.File != name {
			return errors.New("unexpected feed platform")
		}
		data, err := readBounded(filepath.Join(o.Updates, name), 1<<20)
		if err != nil {
			return err
		}
		if err = validateRecord(entry, data, public); err != nil {
			return err
		}
		if strings.HasPrefix(channel, "macos") {
			data, err = verifyMacFeed(data, public)
			if err != nil {
				return err
			}
		} else {
			detached, err := readBounded(filepath.Join(o.Updates, name+".signature"), 4096)
			if err != nil || string(detached) != entry.Signature+"\n" {
				return errors.New("feed signature is missing or differs")
			}
		}
		if channel == "linux-x64" {
			signature, _ := base64.StdEncoding.Strict().DecodeString(entry.Signature)
			linux, err := update.ParseManifest(data, signature, public)
			if err != nil {
				return err
			}
			for j, a := range linux.Packages {
				p := m.Packages[j+4]
				if a.File != p.File || a.Length != p.Length || a.SHA256 != p.SHA256 || a.Signature != p.Signature {
					return errors.New("Linux feed differs from signed packages")
				}
			}
		} else if err = verifyEnclosure(data, channel, o, m.Packages[i]); err != nil {
			return err
		}
	}
	sums, err := os.ReadFile(filepath.Join(o.Dist, "SHA256SUMS.txt"))
	if err != nil {
		return err
	}
	if string(sums) != string(betaSums(m, body, encoded)) {
		return errors.New("checksums differ from the signed release")
	}
	return nil
}
