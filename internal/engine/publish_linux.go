package engine

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

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
