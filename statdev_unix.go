//go:build !windows

package main

import (
	"os"
	"syscall"
)

// statDevice returns the device a path lives on, the number the kernel gives
// each mounted file system. Callers only compare ids for equality, to tell
// whether two paths share a volume.
func statDevice(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, false
	}
	return uint64(st.Dev), true
}
