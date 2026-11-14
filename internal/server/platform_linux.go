package server

import (
	"os"
	"strings"
)

const devicePlatform = "linux"

// SMBIOS chassis types: portable, laptop, notebook, sub notebook, convertible,
// detachable and similar portable designs.
func detectLaptop() bool {
	raw, err := os.ReadFile("/sys/class/dmi/id/chassis_type")
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(raw)) {
	case "8", "9", "10", "11", "14", "30", "31", "32":
		return true
	}
	return false
}
