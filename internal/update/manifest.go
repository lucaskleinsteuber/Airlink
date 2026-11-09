// Package update verifies the public Linux beta channel. It has no LAN routes
// and never reads a publishing key or a transfer host's private identity.
package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
)

const PublicKeyBase64 = "Qv0ebRzPio5qS8peZP0RnKqPe+J8mcE0LO3haWpcEjg="
const PackageLimit int64 = 512 << 20

type Artifact struct {
	Type      string `json:"type"`
	File      string `json:"file"`
	URL       string `json:"url"`
	Length    int64  `json:"length"`
	SHA256    string `json:"sha256"`
	Signature string `json:"ed25519Signature"`
}
type Manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Channel       string     `json:"channel"`
	Platform      string     `json:"platform"`
	Architecture  string     `json:"architecture"`
	Version       string     `json:"version"`
	Packages      []Artifact `json:"packages"`
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$`)

func VersionParts(value string) ([4]uint64, error) {
	var result [4]uint64
	m := versionPattern.FindStringSubmatch(value)
	if m == nil {
		return result, errors.New("invalid release version")
	}
	for i := 0; i < 3; i++ {
		v, e := strconv.ParseUint(m[i+1], 10, 64)
		if e != nil {
			return result, e
		}
		result[i] = v
	}
	result[3] = ^uint64(0)
	if m[4] != "" {
		v, e := strconv.ParseUint(m[4], 10, 64)
		if e != nil || v == ^uint64(0) {
			return result, errors.New("invalid beta number")
		}
		result[3] = v
	}
	return result, nil
}
func CompareVersions(a, b string) (int, error) {
	x, e := VersionParts(a)
	if e != nil {
		return 0, e
	}
	y, e := VersionParts(b)
	if e != nil {
		return 0, e
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, nil
		}
		if x[i] > y[i] {
			return 1, nil
		}
	}
	return 0, nil
}
func ParseManifest(body, signature []byte, public ed25519.PublicKey) (Manifest, error) {
	var m Manifest
	if len(body) == 0 || len(body) > 1<<20 || len(public) != 32 || !ed25519.Verify(public, body, signature) {
		return m, errors.New("invalid update feed signature")
	}
	if json.Unmarshal(body, &m) != nil || m.SchemaVersion != 1 || m.Channel != "beta" || m.Platform != "linux" || m.Architecture != "x64" {
		return m, errors.New("unsupported update feed")
	}
	if _, e := VersionParts(m.Version); e != nil {
		return m, e
	}
	formats := map[string]string{"appimage": ".AppImage", "deb": ".deb", "rpm": ".rpm", "arch": ".pkg.tar.zst"}
	seen := map[string]bool{}
	for _, a := range m.Packages {
		ext, ok := formats[a.Type]
		name := "AirLink-" + m.Version + "-linux-x64" + ext
		sig, e := base64.StdEncoding.Strict().DecodeString(a.Signature)
		sum, se := hex.DecodeString(a.SHA256)
		if !ok || seen[a.Type] || a.File != name || a.URL != "https://github.com/lucaskleinsteuber/Airlink/releases/download/v"+m.Version+"/"+name || a.Length <= 0 || a.Length > PackageLimit || e != nil || len(sig) != 64 || se != nil || len(sum) != 32 || hex.EncodeToString(sum) != a.SHA256 {
			return m, errors.New("invalid update package metadata")
		}
		seen[a.Type] = true
	}
	if len(seen) != 4 {
		return m, errors.New("incomplete update feed")
	}
	return m, nil
}
func OpenRegular(path string, limit int64) (*os.File, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("update file missing, linked, empty or oversized")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	actual, e := f.Stat()
	if e != nil || !os.SameFile(info, actual) {
		f.Close()
		return nil, errors.New("update file changed")
	}
	return f, nil
}
func ReadRegular(path string, limit int64) ([]byte, error) {
	f, e := OpenRegular(path, limit)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("update file exceeded its bound")
	}
	return b, nil
}
func VerifyArtifact(path string, a Artifact, public ed25519.PublicKey) error {
	f, e := OpenRegular(path, PackageLimit)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || info.Size() != a.Length {
		return errors.New("update package length differs")
	}
	h := sha256.New()
	if _, e = io.CopyBuffer(h, f, make([]byte, 128<<10)); e != nil {
		return e
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != a.SHA256 {
		return errors.New("update package checksum differs")
	}
	signature, e := base64.StdEncoding.Strict().DecodeString(a.Signature)
	if e != nil {
		return errors.New("invalid package signature encoding")
	}
	return verifyMapped(f, info.Size(), signature, public)
}
