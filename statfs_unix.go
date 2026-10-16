//go:build darwin || linux

package main

import "syscall"

// volumeCap reports a volume's free and total bytes for the 此电脑 page's
// drive tiles.
func volumeCap(path string) (free, total int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := int64(st.Bsize)
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, true
}
