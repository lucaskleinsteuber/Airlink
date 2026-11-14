package transfer

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func syncData(f *os.File) error { return unix.Fdatasync(int(f.Fd())) }

func barrier(*os.File) error { return nil }

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
	return unix.Renameat2(int(from.Fd()), filepath.Base(oldName), int(to.Fd()), filepath.Base(newName), unix.RENAME_NOREPLACE)
}

func hideDirectory(*os.Root, string) error { return nil }
