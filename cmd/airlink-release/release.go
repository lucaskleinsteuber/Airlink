package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const packageLimit = 512 << 20
const manifestName = "UPDATE-SIGNATURES.json"
const signingPrefix = "<!-- sparkle-signatures:\n"

type artifactRecord struct {
	File      string `json:"file"`
	Length    int64  `json:"length"`
	SHA256    string `json:"sha256"`
	Signature string `json:"ed25519Signature"`
}

type releaseManifest struct {
	SchemaVersion  int              `json:"schemaVersion"`
	Version        string           `json:"version"`
	Build          string           `json:"macOSBuild"`
	Published      string           `json:"published"`
	KeyFingerprint string           `json:"publicKeySHA256"`
	Packages       []artifactRecord `json:"packages"`
	Feeds          []artifactRecord `json:"feeds"`
}

type releaseOptions struct {
	Version, Build, Dist, Updates, PublicConfig, KeyDirectory, Repository string
	Published                                                             time.Time
	Notes                                                                 string
}

func validateRelease(version, build string) error {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		return errors.New("release version must be Major.Minor.Patch")
	}
	if value, err := strconv.ParseUint(build, 10, 32); err != nil || value == 0 || strconv.FormatUint(value, 10) != build {
		return errors.New("macOS build must be a positive integer")
	}
	return nil
}

func packageNames(version string) []string {
	return []string{
		"AirLink-" + version + "-macos-universal.dmg",
		"AirLink-" + version + "-windows-x64-setup.exe",
		"AirLink-" + version + "-windows-arm64-setup.exe",
	}
}

// Used only to validate and curate metadata from the former five-package
// publication policy. New releases publish the three native packages above.
func legacyPackageNames(version string) []string {
	return append(packageNames(version), "airlink-"+version+"-linux-amd64.tar.gz", "airlink-"+version+"-linux-arm64.tar.gz")
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("required release file is missing, empty, linked, or oversized")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("required release file could not be opened")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit || len(data) == 0 {
		return nil, errors.New("required release file could not be read within its bound")
	}
	return data, nil
}

func record(name string, data []byte, private ed25519.PrivateKey) artifactRecord {
	digest := sha256.Sum256(data)
	return artifactRecord{name, int64(len(data)), hex.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(ed25519.Sign(private, data))}
}

func validateRecord(entry artifactRecord, data []byte, public ed25519.PublicKey) error {
	digest := sha256.Sum256(data)
	signature, err := base64.StdEncoding.Strict().DecodeString(entry.Signature)
	if entry.Length != int64(len(data)) || entry.SHA256 != hex.EncodeToString(digest[:]) || err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(public, data, signature) {
		return errors.New("release content hash, length, or Ed25519 signature is invalid")
	}
	return nil
}

func escaped(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func feedXML(options releaseOptions, platform string, entry artifactRecord) []byte {
	mac := platform == "macos"
	version, signatureAttribute, media := options.Version, "signature", "application/octet-stream"
	if mac {
		version, signatureAttribute, media = options.Build, "edSignature", "application/x-apple-diskimage"
	}
	var out strings.Builder
	fmt.Fprintf(&out, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rss version=\"2.0\" xmlns:sparkle=\"%s\">\n  <channel>\n", sparkleNamespace)
	fmt.Fprintf(&out, "    <title>AirLink updates</title>\n    <link>%s%s.xml</link>\n    <description>Signed AirLink updates.</description>\n    <language>en</language>\n", feedBase, platform)
	fmt.Fprintf(&out, "    <item>\n      <title>AirLink %s</title>\n      <description>%s</description>\n      <pubDate>%s</pubDate>\n", escaped(options.Version), escaped(options.Notes), options.Published.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&out, "      <sparkle:version>%s</sparkle:version>\n      <sparkle:shortVersionString>%s</sparkle:shortVersionString>\n", version, escaped(options.Version))
	if mac {
		out.WriteString("      <sparkle:minimumSystemVersion>14.0</sparkle:minimumSystemVersion>\n")
	}
	operatingSystem := "windows"
	if mac {
		operatingSystem = "macos"
	}
	fmt.Fprintf(&out, "      <enclosure url=\"%sv%s/%s\" length=\"%d\" type=\"%s\" sparkle:version=\"%s\" sparkle:shortVersionString=\"%s\" sparkle:os=\"%s\" sparkle:%s=\"%s\" />\n", releaseBase, options.Version, entry.File, entry.Length, media, version, options.Version, operatingSystem, signatureAttribute, entry.Signature)
	out.WriteString("    </item>\n  </channel>\n</rss>\n")
	return []byte(out.String())
}

// Sparkle 2.10 common_cli/Signing.swift signs the exact UTF-8 content bytes and
// appends this block. It is verified before XML parsing with SURequireSignedFeed.
func signedMacFeed(content []byte, private ed25519.PrivateKey) []byte {
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, content))
	block := fmt.Sprintf("%sedSignature: %s\nlength: %d\n-->\n", signingPrefix, signature, len(content))
	return append(append([]byte{}, content...), []byte(block)...)
}

func verifyMacFeed(data []byte, public ed25519.PublicKey) ([]byte, error) {
	start := bytes.LastIndex(data, []byte(signingPrefix))
	if start < 0 || bytes.Count(data, []byte(signingPrefix)) != 1 {
		return nil, errors.New("macOS update feed is missing its unique signature block")
	}
	lines := strings.Split(string(data[start+len(signingPrefix):]), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "edSignature: ") || !strings.HasPrefix(lines[1], "length: ") || lines[2] != "-->" || lines[3] != "" {
		return nil, errors.New("macOS update feed signature framing is invalid")
	}
	length, err := strconv.ParseUint(strings.TrimPrefix(lines[1], "length: "), 10, 64)
	if err != nil || length != uint64(start) {
		return nil, errors.New("macOS update feed signed length is invalid")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(lines[0], "edSignature: "))
	if err != nil || !ed25519.Verify(public, data[:start], signature) {
		return nil, errors.New("macOS update feed signature is invalid")
	}
	return data[:start], nil
}

func atomicPublicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return errors.New("release output directory could not be created")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".airlink-signing-*")
	if err != nil {
		return errors.New("release output could not be prepared")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return errors.New("release output could not be written")
	}
	if err := file.Chmod(0644); err != nil {
		file.Close()
		return errors.New("release output permissions could not be set")
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return errors.New("release output could not be saved")
	}
	if err := file.Close(); err != nil {
		return errors.New("release output could not be closed")
	}
	if err := os.Rename(temporary, path); err != nil {
		return errors.New("release output could not be published")
	}
	return nil
}

func signRelease(options releaseOptions) error {
	if err := validateRelease(options.Version, options.Build); err != nil {
		return err
	}
	if err := outsideRepository(options.KeyDirectory, options.Repository); err != nil {
		return err
	}
	_, public, err := publicKey(options.PublicConfig)
	if err != nil {
		return err
	}
	private, err := privateSeed(options.KeyDirectory, false)
	if err != nil {
		return err
	}
	if !private.Public().(ed25519.PublicKey).Equal(public) {
		return errors.New("private key does not match embedded update public key")
	}
	if options.Published.IsZero() {
		return errors.New("release publication timestamp is required")
	}
	if options.Notes == "" {
		options.Notes = "AirLink " + options.Version + " update."
	}
	manifest := releaseManifest{SchemaVersion: 1, Version: options.Version, Build: options.Build, Published: options.Published.UTC().Format(time.RFC3339), KeyFingerprint: fingerprint(public)}
	for _, name := range packageNames(options.Version) {
		data, err := readBounded(filepath.Join(options.Dist, name), packageLimit)
		if err != nil {
			return err
		}
		manifest.Packages = append(manifest.Packages, record(name, data, private))
	}
	outputs := make(map[string][]byte)
	for index, platform := range []string{"macos", "windows-x64", "windows-arm64"} {
		content := feedXML(options, platform, manifest.Packages[index])
		name := platform + ".xml"
		if platform == "macos" {
			content = signedMacFeed(content, private)
		}
		outputs[name] = content
		entry := record(name, content, private)
		manifest.Feeds = append(manifest.Feeds, entry)
		if platform != "macos" {
			outputs[name+".signature"] = []byte(entry.Signature + "\n")
		}
	}
	for name, data := range outputs {
		if err := atomicPublicFile(filepath.Join(options.Updates, name), data); err != nil {
			return err
		}
	}
	if err := writeReleaseMetadata(options, manifest); err != nil {
		return err
	}
	return verifyRelease(options)
}

func writeReleaseMetadata(options releaseOptions, manifest releaseManifest) error {
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestData = append(manifestData, '\n')
	if err := atomicPublicFile(filepath.Join(options.Dist, manifestName), manifestData); err != nil {
		return err
	}
	// Every package and the signature manifest is covered by the release sums.
	// The signed manifest separately records exact feed hashes and signatures.
	var sums strings.Builder
	for _, entry := range manifest.Packages {
		fmt.Fprintf(&sums, "%s  %s\n", entry.SHA256, entry.File)
	}
	digest := sha256.Sum256(manifestData)
	fmt.Fprintf(&sums, "%x  %s\n", digest, manifestName)
	if err := atomicPublicFile(filepath.Join(options.Dist, "SHA256SUMS.txt"), []byte(sums.String())); err != nil {
		return err
	}
	return nil
}

type enclosure struct {
	URL             string `xml:"url,attr"`
	Length          int64  `xml:"length,attr"`
	Version         string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version,attr"`
	ShortVersion    string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString,attr"`
	OperatingSystem string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle os,attr"`
	Signature       string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle signature,attr"`
	EdSignature     string `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle edSignature,attr"`
}

type feedDocument struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Version       string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle version"`
			ShortVersion  string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle shortVersionString"`
			MinimumSystem string      `xml:"http://www.andymatuschak.org/xml-namespaces/sparkle minimumSystemVersion"`
			Enclosures    []enclosure `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func verifyEnclosure(content []byte, platform string, options releaseOptions, entry artifactRecord) error {
	var feed feedDocument
	if xml.Unmarshal(content, &feed) != nil || len(feed.Channel.Items) != 1 || len(feed.Channel.Items[0].Enclosures) != 1 {
		return errors.New("update feed must contain one unambiguous update enclosure")
	}
	item := feed.Channel.Items[0]
	download := item.Enclosures[0]
	expectedVersion, expectedOS := options.Version, "windows"
	if platform == "macos" {
		expectedVersion, expectedOS = options.Build, "macos"
	}
	if download.URL != releaseBase+"v"+options.Version+"/"+entry.File || download.Length != entry.Length || download.Version != expectedVersion || download.ShortVersion != options.Version || download.OperatingSystem != expectedOS || item.Version != expectedVersion || item.ShortVersion != options.Version {
		return errors.New("update enclosure does not match the signed release package and version")
	}
	if platform == "macos" {
		if download.EdSignature != entry.Signature || download.Signature != "" || item.MinimumSystem != "14.0" {
			return errors.New("macOS update enclosure signature or system requirement is invalid")
		}
	} else if download.Signature != entry.Signature || download.EdSignature != "" {
		return errors.New("Windows update enclosure signature is invalid")
	}
	return nil
}

func loadReleaseManifest(options releaseOptions, allowLegacy bool) (releaseManifest, []byte, ed25519.PublicKey, error) {
	var manifest releaseManifest
	if err := validateRelease(options.Version, options.Build); err != nil {
		return manifest, nil, nil, err
	}
	_, public, err := publicKey(options.PublicConfig)
	if err != nil {
		return manifest, nil, nil, err
	}
	data, err := readBounded(filepath.Join(options.Dist, manifestName), 1024*1024)
	if err != nil {
		return manifest, nil, nil, err
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.SchemaVersion != 1 || manifest.Version != options.Version || manifest.Build != options.Build || manifest.KeyFingerprint != fingerprint(public) || len(manifest.Feeds) != 3 {
		return manifest, nil, nil, errors.New("release signature manifest is invalid or belongs to another key/version")
	}
	if _, err := time.Parse(time.RFC3339, manifest.Published); err != nil {
		return manifest, nil, nil, errors.New("release manifest publication timestamp is invalid")
	}
	if len(manifest.Packages) != len(packageNames(options.Version)) && !(allowLegacy && len(manifest.Packages) == len(legacyPackageNames(options.Version))) {
		return manifest, nil, nil, errors.New("release manifest must contain only the three native packages; curate legacy metadata explicitly with native-only")
	}
	return manifest, data, public, nil
}

func verifyManifestFiles(options releaseOptions, manifest releaseManifest, data []byte, public ed25519.PublicKey) error {
	names := packageNames(options.Version)
	nativeCount := len(names)
	if len(manifest.Packages) > nativeCount {
		names = legacyPackageNames(options.Version)
	}
	for index, entry := range manifest.Packages {
		if entry.File != names[index] {
			return errors.New("release manifest contains an unexpected package path")
		}
		if index >= nativeCount {
			continue
		} // Removed Linux payloads are not needed to validate retained native content.
		payload, err := readBounded(filepath.Join(options.Dist, entry.File), packageLimit)
		if err != nil {
			return err
		}
		if err := validateRecord(entry, payload, public); err != nil {
			return err
		}
	}
	for index, platform := range []string{"macos", "windows-x64", "windows-arm64"} {
		entry := manifest.Feeds[index]
		if entry.File != platform+".xml" {
			return errors.New("release manifest contains an unexpected feed path")
		}
		feed, err := readBounded(filepath.Join(options.Updates, entry.File), 1024*1024)
		if err != nil {
			return err
		}
		if err := validateRecord(entry, feed, public); err != nil {
			return err
		}
		if platform == "macos" {
			feed, err = verifyMacFeed(feed, public)
			if err != nil {
				return err
			}
		} else {
			detached, err := readBounded(filepath.Join(options.Updates, entry.File+".signature"), 1024)
			if err != nil || string(detached) != entry.Signature+"\n" {
				return errors.New("Windows update feed detached signature is missing or invalid")
			}
		}
		if err := verifyEnclosure(feed, platform, options, manifest.Packages[index]); err != nil {
			return err
		}
	}
	sums, err := readBounded(filepath.Join(options.Dist, "SHA256SUMS.txt"), 16384)
	if err != nil {
		return err
	}
	var expectedSums []string
	for _, entry := range manifest.Packages {
		expectedSums = append(expectedSums, entry.SHA256+"  "+entry.File)
	}
	digest := sha256.Sum256(data)
	expectedSums = append(expectedSums, fmt.Sprintf("%x  %s", digest, manifestName))
	actualSums := strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n")
	sort.Strings(expectedSums)
	sort.Strings(actualSums)
	if strings.Join(expectedSums, "\n") != strings.Join(actualSums, "\n") {
		return errors.New("release checksum list does not cover the final signed package set")
	}
	return nil
}

func verifyRelease(options releaseOptions) error {
	manifest, data, public, err := loadReleaseManifest(options, false)
	if err != nil {
		return err
	}
	return verifyManifestFiles(options, manifest, data, public)
}

// nativeOnly curates existing release metadata with the public key only. It
// never signs, rewrites feeds, reads the private seed, or changes package files.
func nativeOnly(options releaseOptions) error {
	manifest, data, public, err := loadReleaseManifest(options, true)
	if err != nil {
		return err
	}
	if err := verifyManifestFiles(options, manifest, data, public); err != nil {
		return err
	}
	if len(manifest.Packages) == len(packageNames(options.Version)) {
		return nil
	}
	manifest.Packages = manifest.Packages[:len(packageNames(options.Version))]
	if err := writeReleaseMetadata(options, manifest); err != nil {
		return err
	}
	return verifyRelease(options)
}
