//go:build !linux

package update

import "errors"

func ApplyLinux(ApplyOptions) error {
	return errors.New("AppImage updates are available only on Linux x64")
}
