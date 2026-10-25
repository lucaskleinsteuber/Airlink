package engine

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func hidePrivateDirectory(root *os.Root, relative string) error {
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
