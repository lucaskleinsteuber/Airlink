//go:build darwin || linux

package engine

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

func managedIdentity(_ *os.File, info fs.FileInfo) (string, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrConflict
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}
