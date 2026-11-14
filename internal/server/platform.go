package server

import "sync"

// The device symbol shown by other computers: platform (macos, linux,
// windows) and form factor (laptop or desktop). Detection never fails; an
// unknown form factor is reported as desktop.
var localDevice = sync.OnceValues(func() (string, string) {
	if detectLaptop() {
		return devicePlatform, "laptop"
	}
	return devicePlatform, "desktop"
})

func validPlatform(v string) bool { return v == "" || v == "macos" || v == "linux" || v == "windows" }
func validForm(v string) bool     { return v == "" || v == "laptop" || v == "desktop" }
