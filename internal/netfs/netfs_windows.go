//go:build windows

package netfs

import (
	"syscall"
	"unsafe"
)

// driveRemote is GetDriveTypeW's DRIVE_REMOTE: a mapped network drive.
const driveRemote = 4

// kernel32 is this package's own handle to the DLL: the main package's
// kernel32 is not visible across the package boundary, and a lazy DLL is
// cheap enough to hold one per package that needs it.
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var procGetDriveTypeW = kernel32.NewProc("GetDriveTypeW")

// platformIsNetworkPath asks Windows for the drive letter's type: a mapped
// drive (a \\server\share the user connected) is DRIVE_REMOTE, a local
// disk, removable or network-shortcut path is not.
func platformIsNetworkPath(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		// A UNC path (\\server\share) is a network path by construction.
		if len(path) >= 2 && path[0] == '\\' && path[1] == '\\' {
			return true
		}
		return false
	}
	root := path[:3] // "C:\" — GetDriveTypeW wants the root
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	r, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(ptr)))
	return int(r) == driveRemote
}
