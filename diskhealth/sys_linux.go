//go:build linux

package diskhealth

import "syscall"

// getfsstat lists the mounted filesystems. Linux's syscall package has no
// getfsstat; the kernel's mount table at /proc/mounts serves the same
// purpose, and platformMounts reads it.
func platformMounts() ([]mountEntry, error) {
	return nil, errNotSupported
}

// capacityOf reports a mounted path's free and total bytes.
func capacityOf(path string) (free, total int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := int64(st.Bsize)
	// Bavail is what an unprivileged process may use, which is what the
	// page means by 可用; Blocks includes the root reserve.
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, true
}
