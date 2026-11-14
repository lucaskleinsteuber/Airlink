package server

import (
	"syscall"
	"unsafe"
)

const devicePlatform = "windows"

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// A computer with a system battery is treated as a laptop (flag 128 means
// "no system battery", 255 "unknown").
func detectLaptop() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemPowerStatus")
	var status systemPowerStatus
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&status))); r == 0 {
		return false
	}
	return status.BatteryFlag&128 == 0 && status.BatteryFlag != 255
}
