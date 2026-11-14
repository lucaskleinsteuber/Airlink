package transfer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzLocalPath(f *testing.F) {
	for _, seed := range []string{"a/b.txt", "../x", "con.txt", "a:b?/c*d", "Büro/x", ".airlink-partials/x", "a\\..\\b", "name. /x "} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, p string) {
		if validWirePath(p) != nil {
			return
		}
		for _, goos := range []string{"linux", "darwin", "windows"} {
			local := localPath(p, goos)
			if !utf8.ValidString(local) || strings.HasPrefix(local, "/") {
				t.Fatalf("%s: %q -> %q", goos, p, local)
			}
			for _, part := range strings.Split(local, "/") {
				if part == "" || part == "." || part == ".." || len(part) > maxComponentBytes || strings.EqualFold(part, partialsDir) || strings.ContainsRune(part, 0) {
					t.Fatalf("%s: unsafe component %q from %q", goos, part, p)
				}
				if goos == "windows" && (strings.ContainsAny(part, `<>:"\|?*`) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ")) {
					t.Fatalf("windows: invalid component %q from %q", part, p)
				}
			}
		}
	})
}

func TestLongWindowsNameNeverEndsInDotOrSpace(t *testing.T) {
	name := strings.Repeat("a", 253) + " b.txt"
	got := localComponent(name, "windows")
	if len(got) > maxComponentBytes || strings.HasSuffix(got, " ") || strings.HasSuffix(got, ".") {
		t.Fatalf("%q", got)
	}
	if got := localComponent(strings.Repeat("ä", 200), "linux"); len(got) > maxComponentBytes || !utf8.ValidString(got) {
		t.Fatal("multi-byte truncation")
	}
}
