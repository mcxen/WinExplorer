//go:build darwin

package main

import (
	"syscall"
	"time"
)

// birthTime reads a file's creation (birth) time on macOS via the
// st_birthtimespec field of Stat_t.
func birthTime(path string) (time.Time, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return time.Time{}, err
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), nil
}
