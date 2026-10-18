//go:build darwin

package diskhealth

import "syscall"

// getfsstat lists the mounted filesystems. Darwin's syscall package has no
// wrapper for it, and the caller asks for the count first so the buffer is
// the right size.
func getfsstat() ([]mountEntry, error) {
	n, err := syscall.Getfsstat(nil, 2 /* MNT_NOWAIT */)
	if err != nil || n <= 0 {
		return nil, err
	}
	buf := make([]syscall.Statfs_t, n)
	n, err = syscall.Getfsstat(buf, 2)
	if err != nil {
		return nil, err
	}
	out := make([]mountEntry, 0, n)
	for i := 0; i < n; i++ {
		st := &buf[i]
		out = append(out, mountEntry{
			From: cString(st.Mntfromname[:]),
			On:   cString(st.Mntonname[:]),
			FS:   cString(st.Fstypename[:]),
		})
	}
	return out, nil
}

// platformMounts is getfsstat's table, the same one readMounts uses.
func platformMounts() ([]mountEntry, error) { return getfsstat() }

// cString reads a NUL-terminated byte array the kernel filled in.
func cString(b []int8) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
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
