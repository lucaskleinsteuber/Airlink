package transfer

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func syncData(f *os.File) error { return f.Sync() }

func barrier(*os.File) error { return nil }

// NTFS journals directory entries; FlushFileBuffers rejects directory handles.
func syncDir(*os.File) error { return nil }

func renameNoReplace(root *os.Root, oldName, newName string) error {
	for _, name := range []string{oldName, newName} {
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			info, err := root.Lstat(parent)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: destination contains a link", ErrConflict)
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

func hideDirectory(root *os.Root, relative string) error {
	name, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), relative))
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(name, attributes|windows.FILE_ATTRIBUTE_HIDDEN)
}
