package transfer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Wire paths are relative, slash separated and may use any characters a
// sender's file system allows. A receiver maps them to names that are valid
// and safe on its own platform; it never trusts a path for traversal.

const maxComponentBytes = 255
const maxPathBytes = 4096

// validWirePath rejects paths that are not plain relative paths.
func validWirePath(p string) error {
	if p == "" || len(p) > maxPathBytes || !utf8.ValidString(p) || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return fmt.Errorf("%w: unsafe relative path", ErrInvalid)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || len(part) > maxComponentBytes {
			return fmt.Errorf("%w: unsafe path component", ErrInvalid)
		}
	}
	return nil
}

// localComponent maps one received name component to a safe local name.
func localComponent(name, goos string) string {
	name = composeLatin(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		case goos == "windows" && strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	// Leave room for a "_" prefix; trim before the Windows rules so a cut
	// can never end in a dot or space there.
	for len(out) > maxComponentBytes-1 {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	if goos == "windows" {
		out = strings.TrimRight(out, ". ")
		stem := strings.ToUpper(strings.SplitN(out, ".", 2)[0])
		switch stem {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			out = "_" + out
		default:
			if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9' {
				out = "_" + out
			}
		}
	}
	if strings.EqualFold(out, partialsDir) {
		out = "_" + out
	}
	if out == "" || out == "." || out == ".." {
		out = "_"
	}
	return out
}

func localPath(p, goos string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = localComponent(part, goos)
	}
	return strings.Join(parts, "/")
}

// numberedName inserts " (n)" before a file extension, or at the end of a
// folder name: "Report (2).pdf", "Photos (2)".
func numberedName(name string, n int, directory bool) string {
	ext := ""
	if !directory && !strings.HasPrefix(name, ".") {
		ext = path.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
	}
	stem := strings.TrimSuffix(name, ext)
	suffix := fmt.Sprintf(" (%d)", n)
	for len(stem)+len(suffix)+len(ext) > maxComponentBytes && len(stem) > 0 {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + suffix + ext
}

// destinationExists reports whether a name is taken, case-insensitively,
// without following links.
func destinationExists(root *os.Root, rel string) (bool, error) {
	_, err := root.Lstat(filepath.FromSlash(rel))
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	// Case-sensitive file systems would allow "Photo.JPG" next to "photo.jpg";
	// the other desktop platforms would not, so keep names distinct anyway.
	if runtime.GOOS == "linux" {
		dir, err := root.Open(filepath.FromSlash(path.Dir(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		defer dir.Close()
		base := path.Base(rel)
		for {
			names, err := dir.Readdirnames(512)
			for _, name := range names {
				if strings.EqualFold(name, base) {
					return true, nil
				}
			}
			if err != nil {
				break
			}
		}
	}
	return false, nil
}

// combining maps (base, combining mark) to the precomposed character. It
// covers Latin letters used in European languages, which is what macOS
// decomposed (NFD) names contain in practice.
var combining = map[[2]rune]rune{}

func init() {
	table := map[rune]string{
		0x0300: "AÀEÈIÌOÒUÙaàeèiìoòuùNǸnǹWẀwẁYỲyỳ",                     // grave
		0x0301: "AÁEÉIÍOÓUÚYÝaáeéiíoóuúyýCĆcćNŃnńSŚsśZŹzźLĹlĺRŔrŕGǴgǵ", // acute
		0x0302: "AÂEÊIÎOÔUÛaâeêiîoôuûCĈcĉGĜgĝHĤhĥJĴjĵSŜsŝWŴwŵYŶyŷ",     // circumflex
		0x0303: "AÃOÕNÑaãoõnñIĨiĩUŨuũ",                                 // tilde
		0x0308: "AÄEËIÏOÖUÜaäeëiïoöuüyÿYŸ",                             // diaeresis
		0x030A: "AÅaåUŮuů",                                             // ring
		0x0327: "CÇcçSŞsşTŢtţGĢgģKĶkķLĻlļNŅnņRŖrŗ",                     // cedilla
		0x030C: "CČcčSŠsšZŽzžEĚeěRŘrřNŇnňDĎdďTŤtťGǦgǧ",                 // caron
		0x0328: "AĄaąEĘeęIĮiįUŲuų",                                     // ogonek
		0x0307: "ZŻzżEĖeėCĊcċGĠgġIİ",                                   // dot above
		0x0304: "AĀaāEĒeēIĪiīOŌoōUŪuū",                                 // macron
		0x0306: "AĂaăGĞgğUŬuŭ",                                         // breve
		0x030B: "OŐoőUŰuű",                                             // double acute
	}
	for mark, pairs := range table {
		runes := []rune(pairs)
		for i := 0; i+1 < len(runes); i += 2 {
			combining[[2]rune{runes[i], mark}] = runes[i+1]
		}
	}
}

// composeLatin converts common decomposed Latin sequences to their
// precomposed (NFC) form. Unknown sequences are kept unchanged.
func composeLatin(s string) string {
	hasMark := false
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			hasMark = true
			break
		}
	}
	if !hasMark {
		return s
	}
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		if n := len(out); n > 0 && unicode.Is(unicode.Mn, r) {
			if composed, ok := combining[[2]rune{out[n-1], r}]; ok {
				out[n-1] = composed
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}
