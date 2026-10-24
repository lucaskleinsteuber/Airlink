package shares

import (
	"fmt"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
)

func fileIdentity(f *os.File, _ fs.FileInfo) (string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
