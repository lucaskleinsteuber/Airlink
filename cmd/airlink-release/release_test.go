package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) releaseOptions {
	t.Helper()
	root := t.TempDir()
	options := releaseOptions{Version: "0.9.0", Build: "11", Dist: filepath.Join(root, "dist"), Updates: filepath.Join(root, "updates"), PublicConfig: filepath.Join(root, "updates", "public-keys.json"), KeyDirectory: filepath.Join(root, "private"), Repository: ".", Published: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), Notes: "Signed update <notes> & native setup."}
	if err := initialize(options.KeyDirectory, options.PublicConfig, options.Repository); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(options.Dist, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range packageNames(options.Version) {
		if err := os.WriteFile(filepath.Join(options.Dist, name), []byte("Synthetic package: "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return options
}

func legacyFixture(t *testing.T) releaseOptions {
	t.Helper()
	options := fixture(t)
	if err := signRelease(options); err != nil {
		t.Fatal(err)
	}
	private, err := privateSeed(options.KeyDirectory, false)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	data, err := os.ReadFile(filepath.Join(options.Dist, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, name := range legacyPackageNames(options.Version)[3:] {
		payload := []byte("Legacy synthetic Linux package: " + name + "\n")
		if err := os.WriteFile(filepath.Join(options.Dist, name), payload, 0644); err != nil {
			t.Fatal(err)
		}
		manifest.Packages = append(manifest.Packages, record(name, payload, private))
	}
	if err := writeReleaseMetadata(options, manifest); err != nil {
		t.Fatal(err)
	}
	return options
}

func snapshotFiles(t *testing.T, paths []string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = data
	}
	return result
}

func unchangedFiles(t *testing.T, snapshot map[string][]byte) {
	t.Helper()
	for path, before := range snapshot {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("metadata curation changed a preserved file")
		}
	}
}

func TestNewReleasePublishesOnlyThreeNativePackages(t *testing.T) {
	options := fixture(t)
	var legacyPaths []string
	for _, name := range legacyPackageNames(options.Version)[3:] {
		path := filepath.Join(options.Dist, name)
		if err := os.WriteFile(path, []byte("Ignored former package"), 0644); err != nil {
			t.Fatal(err)
		}
		legacyPaths = append(legacyPaths, path)
	}
	snapshot := snapshotFiles(t, legacyPaths)
	if err := signRelease(options); err != nil {
		t.Fatal(err)
	}
	manifest, _, _, err := loadReleaseManifest(options, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Packages) != 3 {
		t.Fatal("new release did not use the native-only package policy")
	}
	sums, err := os.ReadFile(filepath.Join(options.Dist, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sums, []byte("linux")) || len(strings.Split(strings.TrimSpace(string(sums)), "\n")) != 4 {
		t.Fatal("new release checksums include unpublished packages")
	}
	unchangedFiles(t, snapshot)
}

func TestNativeOnlyCuratesLegacyMetadataWithoutPrivateKeyOrResigning(t *testing.T) {
	options := legacyFixture(t)
	if verifyRelease(options) == nil {
		t.Fatal("regular verification silently accepted the obsolete five-package publication policy")
	}
	before, _, _, err := loadReleaseManifest(options, true)
	if err != nil {
		t.Fatal(err)
	}
	var preservedPaths []string
	for _, name := range legacyPackageNames(options.Version) {
		preservedPaths = append(preservedPaths, filepath.Join(options.Dist, name))
	}
	for _, name := range []string{"public-keys.json", "macos.xml", "windows-x64.xml", "windows-x64.xml.signature", "windows-arm64.xml", "windows-arm64.xml.signature"} {
		preservedPaths = append(preservedPaths, filepath.Join(options.Updates, name))
	}
	snapshot := snapshotFiles(t, preservedPaths)
	if err := os.Remove(filepath.Join(options.KeyDirectory, seedName)); err != nil {
		t.Fatal(err)
	}
	options.KeyDirectory = filepath.Join(t.TempDir(), "must-not-be-accessed")
	if err := nativeOnly(options); err != nil {
		t.Fatal(err)
	}
	after, _, _, err := loadReleaseManifest(options, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Published != before.Published || after.Version != before.Version || after.Build != before.Build || after.KeyFingerprint != before.KeyFingerprint || !reflect.DeepEqual(after.Packages, before.Packages[:3]) || !reflect.DeepEqual(after.Feeds, before.Feeds) {
		t.Fatal("curation changed retained native signatures, feeds, timestamp, or release identity")
	}
	unchangedFiles(t, snapshot)
	sums, err := os.ReadFile(filepath.Join(options.Dist, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sums, []byte("linux")) {
		t.Fatal("Linux checksum records survived native-only curation")
	}
	metadata := snapshotFiles(t, []string{filepath.Join(options.Dist, manifestName), filepath.Join(options.Dist, "SHA256SUMS.txt")})
	if err := nativeOnly(options); err != nil {
		t.Fatal("idempotent curation failed:", err)
	}
	unchangedFiles(t, metadata)
}

func TestNativeOnlyDoesNotRequireRemovedLinuxDownloads(t *testing.T) {
	options := legacyFixture(t)
	for _, name := range legacyPackageNames(options.Version)[3:] {
		if err := os.Remove(filepath.Join(options.Dist, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(options.KeyDirectory, seedName)); err != nil {
		t.Fatal(err)
	}
	if err := nativeOnly(options); err != nil {
		t.Fatal("removed Linux payloads were required for native curation:", err)
	}
	if err := verifyRelease(options); err != nil {
		t.Fatal(err)
	}
}

func TestNativeOnlyRejectsTamperingBeforeChangingMetadata(t *testing.T) {
	for _, target := range []string{"package", "macos-feed", "windows-feed", "windows-signature", "manifest", "checksums"} {
		t.Run(target, func(t *testing.T) {
			options := legacyFixture(t)
			paths := map[string]string{
				"package":           filepath.Join(options.Dist, packageNames(options.Version)[1]),
				"macos-feed":        filepath.Join(options.Updates, "macos.xml"),
				"windows-feed":      filepath.Join(options.Updates, "windows-x64.xml"),
				"windows-signature": filepath.Join(options.Updates, "windows-x64.xml.signature"),
				"manifest":          filepath.Join(options.Dist, manifestName),
				"checksums":         filepath.Join(options.Dist, "SHA256SUMS.txt"),
			}
			path := paths[target]
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)/2] ^= 1
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			snapshot := snapshotFiles(t, []string{filepath.Join(options.Dist, manifestName), filepath.Join(options.Dist, "SHA256SUMS.txt")})
			if nativeOnly(options) == nil {
				t.Fatal("native-only curation accepted tampered retained content")
			}
			unchangedFiles(t, snapshot)
		})
	}
}

func TestNativeOnlyCLIUsesPublicConfigurationAndPreservesPublicationMetadata(t *testing.T) {
	options := legacyFixture(t)
	if err := os.Remove(filepath.Join(options.KeyDirectory, seedName)); err != nil {
		t.Fatal(err)
	}
	// Public verification/curation must work without a publishing directory or
	// even a configured user home. Neither command needs the signing seed.
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	arguments := []string{"native-only", "--version", options.Version, "--build", options.Build,
		"--dist", options.Dist, "--updates", options.Updates, "--public-config", options.PublicConfig}
	if err := run(arguments); err != nil {
		t.Fatal(err)
	}
	arguments[0] = "verify"
	if err := run(arguments); err != nil {
		t.Fatal(err)
	}
	arguments[0] = "native-only"
	for _, extra := range [][]string{{"--published", "2026-12-01T00:00:00Z"}, {"--notes-file", "must-not-be-read"}} {
		if run(append(append([]string{}, arguments...), extra...)) == nil {
			t.Fatal("metadata-only curation accepted a timestamp or notes override")
		}
	}
}

func TestRFC8032Ed25519Compatibility(t *testing.T) {
	// Published RFC 8032 section 7.1 test 1, not an application signing secret.
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	public, _ := hex.DecodeString("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	expected, _ := hex.DecodeString("e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e06522490155" + "5fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b")
	private := ed25519.NewKeyFromSeed(seed)
	if !private.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(public)) || !bytes.Equal(ed25519.Sign(private, nil), expected) {
		t.Fatal("standard Ed25519 RFC vector does not match")
	}
}

func TestSignedReleaseRoundTripAndTampering(t *testing.T) {
	for _, target := range []string{"none", "package", "macos-feed", "windows-feed", "windows-signature", "manifest", "checksums"} {
		t.Run(target, func(t *testing.T) {
			options := fixture(t)
			if err := signRelease(options); err != nil {
				t.Fatal(err)
			}
			if target == "none" {
				if err := os.Remove(filepath.Join(options.KeyDirectory, seedName)); err != nil {
					t.Fatal(err)
				}
				if err := verifyRelease(options); err != nil {
					t.Fatal("public-only verification must succeed:", err)
				}
				return
			}
			paths := map[string]string{
				"package":           filepath.Join(options.Dist, packageNames(options.Version)[1]),
				"macos-feed":        filepath.Join(options.Updates, "macos.xml"),
				"windows-feed":      filepath.Join(options.Updates, "windows-x64.xml"),
				"windows-signature": filepath.Join(options.Updates, "windows-x64.xml.signature"),
				"manifest":          filepath.Join(options.Dist, manifestName),
				"checksums":         filepath.Join(options.Dist, "SHA256SUMS.txt"),
			}
			path := paths[target]
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)/2] ^= 1
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if verifyRelease(options) == nil {
				t.Fatal("tampered release content was accepted")
			}
		})
	}
}

func TestMissingFinalPackageDoesNotPublishFeeds(t *testing.T) {
	options := fixture(t)
	if err := os.Remove(filepath.Join(options.Dist, packageNames(options.Version)[2])); err != nil {
		t.Fatal(err)
	}
	if signRelease(options) == nil {
		t.Fatal("incomplete final package set was signed")
	}
	for _, path := range []string{filepath.Join(options.Updates, "macos.xml"), filepath.Join(options.Dist, manifestName)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("incomplete release published signing outputs")
		}
	}
}

func TestInitializationPreservesStableKeyAndRejectsReplacement(t *testing.T) {
	options := fixture(t)
	path := filepath.Join(options.KeyDirectory, seedName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initialize(options.KeyDirectory, options.PublicConfig, options.Repository); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("initialization unexpectedly rotated the stable signing key")
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(options.PublicConfig, configuration(other)); err != nil {
		t.Fatal(err)
	}
	if initialize(options.KeyDirectory, options.PublicConfig, options.Repository) == nil {
		t.Fatal("mismatched public key was silently replaced")
	}
	after, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("mismatched public configuration modified the private seed")
	}
}

func TestMissingSeedCannotRotateExistingPublicIdentity(t *testing.T) {
	options := fixture(t)
	path := filepath.Join(options.KeyDirectory, seedName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if initialize(options.KeyDirectory, options.PublicConfig, options.Repository) == nil {
		t.Fatal("missing stable seed must require recovery")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a replacement key was generated for an existing public identity")
	}
}

func TestPrivateKeyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not describe Windows ACLs")
	}
	options := fixture(t)
	path := filepath.Join(options.KeyDirectory, seedName)
	dirInfo, err := os.Stat(options.KeyDirectory)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 || info.Mode().Perm() != 0600 {
		t.Fatal("new private key permissions are not owner-only")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := privateSeed(options.KeyDirectory, false); err == nil {
		t.Fatal("readable private seed permissions were accepted")
	}
}

func TestPrivateKeysCannotBeStoredInAnyGitCheckout(t *testing.T) {
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if outsideRepository(filepath.Join(current, "private-key-must-not-exist"), current) == nil {
		t.Fatal("repository-local key directory was accepted")
	}
	other := t.TempDir()
	if err := exec.Command("git", "init", "--quiet", other).Run(); err != nil {
		t.Fatal("synthetic Git repository could not be prepared")
	}
	if outsideRepository(filepath.Join(other, "private"), current) == nil {
		t.Fatal("another Git checkout was accepted for private keys")
	}
	link := filepath.Join(t.TempDir(), "repository-link")
	if err := os.Symlink(current, link); err != nil {
		t.Skip("host does not permit a synthetic symlink")
	}
	if outsideRepository(filepath.Join(link, "private"), current) == nil {
		t.Fatal("symlink into the checkout bypassed key isolation")
	}
}

func TestFeedBindsPlatformVersionAndPayload(t *testing.T) {
	options := fixture(t)
	if err := signRelease(options); err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	data, err := os.ReadFile(filepath.Join(options.Dist, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	feed := feedXML(options, "windows-x64", manifest.Packages[1])
	if verifyEnclosure(feed, "windows-arm64", options, manifest.Packages[2]) == nil {
		t.Fatal("wrong-platform update payload was accepted")
	}
	wrong := options
	wrong.Version = "0.9.1"
	if verifyEnclosure(feed, "windows-x64", wrong, manifest.Packages[1]) == nil {
		t.Fatal("enclosure version mismatch was accepted")
	}
	for _, version := range []string{"1.0", "0.9.0-beta", "../0.9.0", "00.9.0"} {
		if validateRelease(version, "11") == nil {
			t.Fatal("invalid semantic release version was accepted")
		}
	}
}

func TestOfficialSparkle210SignatureCompatibility(t *testing.T) {
	tool := os.Getenv("AIRLINK_SPARKLE_SIGN_UPDATE")
	if tool == "" {
		tool = filepath.Join("..", "..", "build", "swift-arm64", "artifacts", "sparkle", "Sparkle", "bin", "sign_update")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("official Sparkle verification CLI requires macOS")
	}
	if _, err := os.Stat(tool); err != nil {
		t.Skip("official Sparkle sign_update CLI is not present")
	}
	options := fixture(t)
	if err := signRelease(options); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(options.KeyDirectory, seedName)
	payload := filepath.Join(options.Dist, packageNames(options.Version)[0])
	output, err := exec.Command(tool, "-f", seed, "-p", payload).Output()
	if err != nil {
		t.Fatal("official Sparkle signer rejected raw32-seed format")
	}
	var manifest releaseManifest
	data, err := os.ReadFile(filepath.Join(options.Dist, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(data, &manifest) != nil || strings.TrimSpace(string(output)) != manifest.Packages[0].Signature {
		t.Fatal("Go signature does not match official Sparkle Ed25519 signature")
	}
	feed := filepath.Join(options.Updates, "macos.xml")
	if err := exec.Command(tool, "--verify", "-f", seed, feed).Run(); err != nil {
		t.Fatal("official Sparkle verifier rejected the signed appcast")
	}
	data, err = os.ReadFile(feed)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("Signed update"), []byte("Forged update"), 1)
	if err := os.WriteFile(feed, data, 0644); err != nil {
		t.Fatal(err)
	}
	if exec.Command(tool, "--verify", "-f", seed, feed).Run() == nil {
		t.Fatal("official Sparkle verifier accepted a tampered appcast")
	}
	if err := os.WriteFile(payload, []byte("tampered package"), 0644); err != nil {
		t.Fatal(err)
	}
	if exec.Command(tool, "--verify", "-f", seed, payload, manifest.Packages[0].Signature).Run() == nil {
		t.Fatal("official Sparkle verifier accepted a tampered update payload")
	}
}
