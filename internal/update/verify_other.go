//go:build !linux

package update

import (
	"crypto/ed25519"
	"errors"
	"io"
	"os"
)

func verifyMapped(f *os.File, size int64, sig []byte, pub ed25519.PublicKey) error {
	if _, e := f.Seek(0, 0); e != nil {
		return e
	}
	b, e := io.ReadAll(io.LimitReader(f, size+1))
	if e != nil || int64(len(b)) != size || len(pub) != 32 || !ed25519.Verify(pub, b, sig) {
		return errors.New("update package signature differs")
	}
	return nil
}
