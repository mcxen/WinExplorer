//go:build !darwin && !linux

package main

// volumeCap is unimplemented off Unix; the 此电脑 page shows no capacity.
func volumeCap(path string) (free, total int64, ok bool) {
	return 0, 0, false
}
