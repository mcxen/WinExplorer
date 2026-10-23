//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
	user32si                     = syscall.NewLazyDLL("user32.dll")
	procFindWindowW              = user32si.NewProc("FindWindowW")
	procShowWindow               = user32si.NewProc("ShowWindow")
	procSetForegroundWindow      = user32si.NewProc("SetForegroundWindow")
	procAllowSetForegroundWindow = user32si.NewProc("AllowSetForegroundWindow")
)

// instanceMutexName is per session ("Local\"): each signed-in user runs
// their own 晴谷.
const instanceMutexName = `Local\com.mcxen.sunvale.single-instance`

const errorAlreadyExists = 183

// osInstanceLock creates the named mutex. The kernel drops it when the
// process ends, so a crash never leaves a stale lock. The handle is kept
// for the process's life.
func osInstanceLock() (bool, func()) {
	name, err := syscall.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return true, nil
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true, nil // no mutex: let the socket decide
	}
	release := func() { procCloseHandle.Call(h) }
	if errno, ok := callErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		// Let the first instance take the foreground when it raises its
		// window for us.
		procAllowSetForegroundWindow.Call(^uintptr(0)) // ASFW_ANY
		return false, release
	}
	return true, nil
}

// raiseRunningInstance finds the first instance's window by its title and
// brings it forward, for when the socket handoff did not happen.
func raiseRunningInstance() {
	title, err := syscall.UTF16PtrFromString("晴谷")
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return
	}
	const swRestore = 9
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
}
