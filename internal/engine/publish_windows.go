package engine

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func renameNoReplace(root *os.Root, oldName, newName string) error {
	// MoveFile refuses an existing target. Validate every component through
	// Root first; remote manifests cannot create links or reparse points.
	for _, name := range []string{oldName, newName} {
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			info, err := root.Lstat(parent)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: destination contains link", ErrConflict)
			}
		}
	}
	from, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), oldName))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), newName))
	if err != nil {
		return err
	}
	return windows.MoveFile(from, to)
}
