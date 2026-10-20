//go:build linux

package search

// platformPrune lists Linux virtual filesystems and mount points that the
// volume roots already cover.
func platformPrune() []string {
	return []string{"/proc", "/sys", "/dev", "/run", "/media", "/mnt"}
}
