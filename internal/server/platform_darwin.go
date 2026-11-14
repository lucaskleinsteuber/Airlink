package server

import (
	"strings"
	"syscall"
)

const devicePlatform = "macos"

func detectLaptop() bool {
	model, err := syscall.Sysctl("hw.model")
	return err == nil && strings.Contains(strings.ToLower(model), "book")
}
