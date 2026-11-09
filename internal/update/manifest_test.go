package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func signedFixture(t *testing.T) (Manifest, []byte, []byte, ed25519.PublicKey, []byte) {
	t.Helper()
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	payload := make([]byte, 8192)
	copy(payload, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 'A', 'I', 2})
	copy(payload[64:], []byte("signed test fixture; not an executable application"))
	sum := sha256.Sum256(payload)
	m := Manifest{SchemaVersion: 1, Channel: "beta", Platform: "linux", Architecture: "x64", Version: "0.12.0-beta.1"}
	for kind, ext := range map[string]string{"appimage": ".AppImage", "deb": ".deb", "rpm": ".rpm", "arch": ".pkg.tar.zst"} {
		name := "AirLink-" + m.Version + "-linux-x64" + ext
		m.Packages = append(m.Packages, Artifact{Type: kind, File: name, URL: "https://github.com/lucaskleinsteuber/Airlink/releases/download/v" + m.Version + "/" + name, Length: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload))})
	}
	body, _ := json.Marshal(m)
	return m, body, ed25519.Sign(key, body), pub, payload
}
func TestSignedManifestAndExactArtifact(t *testing.T) {
	m, body, sig, pub, payload := signedFixture(t)
	if got, e := ParseManifest(body, sig, pub); e != nil || got.Version != m.Version {
		t.Fatal(got, e)
	}
	path := filepath.Join(t.TempDir(), "payload")
	if e := os.WriteFile(path, payload, 0600); e != nil {
		t.Fatal(e)
	}
	if e := VerifyArtifact(path, m.Packages[0], pub); e != nil {
		t.Fatal(e)
	}
	payload[128] ^= 1
	if e := os.WriteFile(path, payload, 0600); e != nil {
		t.Fatal(e)
	}
	if e := VerifyArtifact(path, m.Packages[0], pub); e == nil {
		t.Fatal("modified artifact accepted")
	}
	if _, e := ParseManifest(append(body, ' '), sig, pub); e == nil {
		t.Fatal("modified feed accepted")
	}
	if _, e := ParseManifest(body, sig, ed25519.PublicKey(bytes.Repeat([]byte{1}, 32))); e == nil {
		t.Fatal("wrong publishing key accepted")
	}
	if e := VerifyArtifact(path, Artifact{Length: PackageLimit + 1}, pub); e == nil {
		t.Fatal("oversized artifact accepted")
	}
}
func TestBetaOrderAndMalformedVersions(t *testing.T) {
	for _, pair := range [][2]string{{"0.12.0-beta.10", "0.12.0-beta.2"}, {"0.12.0", "0.12.0-beta.10"}, {"0.12.0-beta.1", "0.11.0"}} {
		if n, e := CompareVersions(pair[0], pair[1]); e != nil || n <= 0 {
			t.Fatal(pair, n, e)
		}
	}
	for _, v := range []string{"0.12.0-beta.0", "0.12.0-beta.01", "0.12", "v0.12.0", "0.12.0-beta.18446744073709551616"} {
		if _, e := VersionParts(v); e == nil {
			t.Fatal("malformed version", v)
		}
	}
}
func TestCanonicalPublicKeyAndPrivateReadiness(t *testing.T) {
	data, e := os.ReadFile(filepath.Join("..", "..", "updates", "public-keys.json"))
	if e != nil {
		t.Fatal(e)
	}
	var config struct {
		PublicKey string `json:"publicKey"`
	}
	if json.Unmarshal(data, &config) != nil || config.PublicKey != PublicKeyBase64 {
		t.Fatal("Linux trust anchor differs from publication configuration")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ready.json")
	if e = WriteReadiness(path, true, ""); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(path)
	if e != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	link := filepath.Join(dir, "linked.json")
	if e = os.Symlink(path, link); e == nil {
		if _, e = ReadRegular(link, 4096); e == nil {
			t.Fatal("linked update input accepted")
		}
	}
	if e = WriteReadiness(filepath.Join(dir, "arbitrary"), true, ""); e == nil {
		t.Fatal("unexpected reply file accepted")
	}
}
