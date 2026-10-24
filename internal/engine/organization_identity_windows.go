package engine

import (
	"fmt"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
)

func managedIdentity(f *os.File, _ fs.FileInfo) (string, error) {
	var st windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &st); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", st.VolumeSerialNumber, st.FileIndexHigh, st.FileIndexLow), nil
}
