//go:build darwin || linux

package transfer

import (
	"fmt"
	"io/fs"
	"syscall"
)

// fileIdentity distinguishes a replaced file that has the same name, size and
// modification time. It comes from lstat data, so collecting it is free.
func fileIdentity(info fs.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return ""
}
