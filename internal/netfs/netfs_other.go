//go:build !darwin && !linux && !windows

package netfs

// platformIsNetworkPath cannot tell a network mount from a local one on
// this platform, so every path is treated as local: the listing stays
// serial and the thumbnails decode eagerly, the safe default.
func platformIsNetworkPath(path string) bool { return false }
