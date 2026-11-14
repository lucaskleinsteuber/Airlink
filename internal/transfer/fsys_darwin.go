package transfer

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// syncData pushes a file's data to the drive. On macOS a plain fsync does not
// flush the drive cache; barrier issues one F_FULLFSYNC per batch instead of
// one per file.
func syncData(f *os.File) error { return unix.Fsync(int(f.Fd())) }

func barrier(f *os.File) error {
	_, err := unix.FcntlInt(f.Fd(), unix.F_FULLFSYNC, 0)
	if err != nil {
		return unix.Fsync(int(f.Fd()))
	}
	return nil
}

func syncDir(f *os.File) error { return unix.Fsync(int(f.Fd())) }

func renameNoReplace(root *os.Root, oldName, newName string) error {
	from, err := root.Open(filepath.Dir(oldName))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := root.Open(filepath.Dir(newName))
	if err != nil {
		return err
	}
	defer to.Close()
	return unix.RenameatxNp(int(from.Fd()), filepath.Base(oldName), int(to.Fd()), filepath.Base(newName), unix.RENAME_EXCL)
}

func hideDirectory(*os.Root, string) error { return nil }
