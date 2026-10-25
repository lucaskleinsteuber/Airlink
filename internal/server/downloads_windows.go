package server

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func defaultReceiveDirectory() string {
	if directory, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0); err == nil && filepath.IsAbs(directory) {
		return directory
	}
	return fallbackReceiveDirectory()
}
