//go:build windows

package diskhealth

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW   = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetLogicalDrives      = modkernel32.NewProc("GetLogicalDrives")
	procGetVolumeInformationW = modkernel32.NewProc("GetVolumeInformationW")
)

// platformMounts lists the machine's ready volumes as mount-table rows,
// so the page's volume logic needs no Windows branch. A drive letter is
// both the device and the mount point there.
func platformMounts() ([]mountEntry, error) {
	mask, _, _ := procGetLogicalDrives.Call()
	var out []mountEntry
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		free, total, ok := capacityOf(root)
		if !ok || total == 0 {
			// An empty card reader or an unready volume answers with no
			// capacity; it is not a drive the page should list twice.
			continue
		}
		_ = free
		fs := volumeFS(root)
		out = append(out, mountEntry{From: root, On: root, FS: fs})
	}
	return out, nil
}

// capacityOf reports a volume root's free and total bytes.
func capacityOf(path string) (free, total int64, ok bool) {
	root, err := syscall.UTF16PtrFromString(volumeRoot(path))
	if err != nil {
		return 0, 0, false
	}
	var f, t uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&f)),
		uintptr(unsafe.Pointer(&t)),
		0)
	if r == 0 {
		return 0, 0, false
	}
	return int64(f), int64(t), true
}

// volumeRoot widens a path to its drive root: "C:\Users" → "C:\".
func volumeRoot(path string) string {
	if len(path) >= 2 && path[1] == ':' {
		return path[:2] + `\`
	}
	return path
}

// volumeFS names a volume's filesystem, e.g. NTFS.
func volumeFS(root string) string {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 16)
	r, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(p)),
		0, 0,
		0, 0, 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
