//go:build linux

package update

import (
	"crypto/ed25519"
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func verifyMapped(f *os.File, size int64, sig []byte, pub ed25519.PublicKey) error {
	b, e := unix.Mmap(int(f.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_PRIVATE)
	if e != nil {
		return e
	}
	defer unix.Munmap(b)
	if len(pub) != 32 || !ed25519.Verify(pub, b, sig) {
		return errors.New("update package signature differs")
	}
	return nil
}
