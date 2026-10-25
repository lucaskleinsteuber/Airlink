//go:build darwin || linux

package server

func defaultReceiveDirectory() string { return fallbackReceiveDirectory() }
