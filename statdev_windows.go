//go:build windows

package main

import (
	"hash/fnv"
	"path/filepath"
)

// statDevice identifies the volume by its name ("C:", `\\server\share`),
// Windows' stand-in for the device number of a Unix mount: two paths share
// an id exactly when they live on the same volume, which is all the callers
// compare. Paths without a volume name report no id.
func statDevice(path string) (uint64, bool) {
	volume := filepath.VolumeName(path)
	if volume == "" {
		return 0, false
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(volume))
	return h.Sum64(), true
}
