//go:build darwin || linux

package engine

import "os"

func hidePrivateDirectory(_ *os.Root, _ string) error { return nil }
