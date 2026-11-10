// airlink-release signs immutable release packages and native update feeds.
// Its private seed lives outside the repository and is never printed.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const seedName = "ed25519.seed"
const feedBase = "https://raw.githubusercontent.com/lucaskleinsteuber/Airlink/main/updates/"
const releaseBase = "https://github.com/lucaskleinsteuber/Airlink/releases/download/"
const sparkleNamespace = "http://www.andymatuschak.org/xml-namespaces/sparkle"

type publicConfiguration struct {
	SchemaVersion int               `json:"schemaVersion"`
	Algorithm     string            `json:"algorithm"`
	PublicKey     string            `json:"publicKey"`
	Feeds         map[string]string `json:"feeds"`
}

func configuration(public ed25519.PublicKey) publicConfiguration {
	return publicConfiguration{1, "Ed25519", base64.StdEncoding.EncodeToString(public), map[string]string{
		"macos": feedBase + "macos.xml", "macos-arm64": feedBase + "macos-arm64.xml", "macos-x64": feedBase + "macos-x64.xml", "linux-x64": feedBase + "linux-x64.json", "windows-x64": feedBase + "windows-x64.xml", "windows-arm64": feedBase + "windows-arm64.xml",
	}}
}

func defaultKeyDirectory() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("publishing configuration directory is unavailable")
	}
	return filepath.Join(root, "AirLink Publishing"), nil
}

func outsideRepository(directory, repository string) error {
	absoluteRepository, err := filepath.Abs(repository)
	if err != nil {
		return errors.New("invalid repository directory")
	}
	resolvedRepository, err := filepath.EvalSymlinks(absoluteRepository)
	if err != nil {
		return errors.New("repository directory is unavailable")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return errors.New("invalid private key directory")
	}
	// Resolve the nearest existing ancestor before creating anything. A symlink
	// pointing into the checkout must not bypass the out-of-repository rule.
	ancestor := absolute
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return errors.New("private key directory cannot be inspected")
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return errors.New("private key directory has no existing ancestor")
		}
		ancestor = parent
	}
	resolvedAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return errors.New("private key ancestor cannot be resolved")
	}
	// Compare actual directory identities rather than path strings: Windows
	// drive aliases, short paths and extended prefixes can name the same tree.
	repositoryInfo, err := os.Stat(resolvedRepository)
	if err != nil {
		return errors.New("repository directory cannot be inspected")
	}
	for current := resolvedAncestor; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return errors.New("private key ancestor cannot be inspected")
		}
		if os.SameFile(repositoryInfo, info) {
			return errors.New("private signing keys must stay outside the repository")
		}
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return errors.New("private signing keys must stay outside every Git repository")
		} else if !os.IsNotExist(err) {
			return errors.New("private key Git marker cannot be inspected")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	// Also detect bare repositories, which do not have a .git marker.
	if _, err := exec.Command("git", "-C", resolvedAncestor, "rev-parse", "--git-dir").Output(); err == nil {
		return errors.New("private signing keys must stay outside every Git repository")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 128 {
		return errors.New("private key directory Git context could not be inspected")
	}
	return nil
}

func privateSeed(directory string, create bool) (ed25519.PrivateKey, error) {
	if create {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, errors.New("private key directory could not be created")
		}
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("private key directory must be a real directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private key directory requires owner-only permissions")
	}
	path := filepath.Join(directory, seedName)
	if create {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			seed := make([]byte, ed25519.SeedSize)
			if _, err = rand.Read(seed); err == nil {
				_, err = file.WriteString(base64.StdEncoding.EncodeToString(seed) + "\n")
			}
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				return nil, errors.New("new signing seed could not be saved")
			}
		} else if !os.IsExist(err) {
			return nil, errors.New("signing seed could not be created")
		}
	}
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 128 {
		return nil, errors.New("signing seed is unavailable or has an invalid format")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("signing seed requires owner-only permissions")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("signing seed could not be read")
	}
	seed, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("signing seed has an invalid format")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func publicKey(path string) (publicConfiguration, ed25519.PublicKey, error) {
	var config publicConfiguration
	data, err := readBounded(path, 16384)
	if err != nil {
		return config, nil, errors.New("public update configuration is unavailable")
	}
	if json.Unmarshal(data, &config) != nil || config.SchemaVersion != 1 || config.Algorithm != "Ed25519" {
		return config, nil, errors.New("unsupported update signing configuration")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(config.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return config, nil, errors.New("invalid public update signing key")
	}
	expected := configuration(key)
	if len(config.Feeds) != len(expected.Feeds) {
		return config, nil, errors.New("unexpected native update feed configuration")
	}
	for name, address := range expected.Feeds {
		if config.Feeds[name] != address {
			return config, nil, errors.New("unexpected native update feed address")
		}
	}
	return config, ed25519.PublicKey(key), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func initialize(directory, publicPath, repository string) error {
	if err := outsideRepository(directory, repository); err != nil {
		return err
	}
	if _, err := os.Lstat(publicPath); err == nil {
		_, existing, err := publicKey(publicPath)
		if err != nil {
			return err
		}
		private, err := privateSeed(directory, false)
		if err != nil || !existing.Equal(private.Public()) {
			return errors.New("existing public signing configuration does not match the private key; no keys were rotated")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errors.New("public signing configuration could not be inspected")
	}
	private, err := privateSeed(directory, true)
	if err != nil {
		return err
	}
	public := private.Public().(ed25519.PublicKey)
	if err := os.MkdirAll(filepath.Dir(publicPath), 0755); err != nil {
		return errors.New("public configuration directory could not be created")
	}
	return writeJSON(publicPath, configuration(public))
}

func fingerprint(public ed25519.PublicKey) string {
	digest := sha256.Sum256(public)
	return hex.EncodeToString(digest[:])
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("choose init, sign, verify, or native-only")
	}
	defaults := ""
	if arguments[0] == "init" || arguments[0] == "sign" {
		var err error
		defaults, err = defaultKeyDirectory()
		if err != nil {
			return err
		}
	}
	flags := flag.NewFlagSet(arguments[0], flag.ContinueOnError)
	directory := flags.String("key-dir", defaults, "private publishing directory outside Git")
	publicPath := flags.String("public-config", "updates/public-keys.json", "canonical public signing configuration")
	repository := flags.String("repository", ".", "repository directory (private keys are rejected inside it)")
	dist := flags.String("dist", "dist", "final immutable release packages directory")
	updates := flags.String("updates", "updates", "output or verification directory for native update feeds")
	version := flags.String("version", "", "release version, optionally with -beta.N")
	build := flags.String("build", "", "macOS CFBundleVersion of the packaged app")
	published := flags.String("published", "", "publication timestamp in RFC3339 (sign defaults to current UTC time)")
	notesFile := flags.String("notes-file", "", "UTF-8 text release notes embedded in the signed feed")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if arguments[0] == "native-only" && (*published != "" || *notesFile != "") {
		return errors.New("native-only preserves publication metadata and feed content; no timestamp or notes override is allowed")
	}
	if arguments[0] == "init" {
		if err := initialize(*directory, *publicPath, *repository); err != nil {
			return err
		}
		fmt.Println("Public update signing configuration is ready; the private seed remains outside Git.")
		return nil
	}
	options := releaseOptions{Version: *version, Build: *build, Dist: *dist, Updates: *updates, PublicConfig: *publicPath, KeyDirectory: *directory, Repository: *repository, Published: time.Now().UTC()}
	if *published != "" {
		parsed, err := time.Parse(time.RFC3339, *published)
		if err != nil {
			return errors.New("publication timestamp must use RFC3339")
		}
		options.Published = parsed
	}
	if *notesFile != "" {
		notes, err := readBounded(*notesFile, 1024*1024)
		if err != nil {
			return errors.New("release notes could not be read")
		}
		options.Notes = string(notes)
	}
	switch arguments[0] {
	case "sign":
		if err := signRelease(options); err != nil {
			return err
		}
		if modernRelease(options.Version) {
			fmt.Println("Signed and verified eight application packages and five update feeds; release checksums include the signature manifest.")
		} else {
			fmt.Println("Signed and verified three native packages and three update feeds; release checksums include the signature manifest.")
		}
	case "verify":
		if err := verifyRelease(options); err != nil {
			return err
		}
		if modernRelease(options.Version) {
			fmt.Println("Verified eight application packages, five signed update feeds, and final release checksums using the public key only.")
		} else {
			fmt.Println("Verified three native packages, three signed update feeds, and final release checksums using the public key only.")
		}
	case "native-only":
		if err := nativeOnly(options); err != nil {
			return err
		}
		fmt.Println("Curated and verified three native packages using the public key only; native payloads, feed bytes, signatures and publication timestamp were preserved.")
	default:
		return errors.New("choose init, sign, verify, or native-only")
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "airlink-release:", err)
		os.Exit(1)
	}
}
