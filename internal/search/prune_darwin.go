//go:build darwin

package search

// platformPrune keeps a root-volume walk from repeating mounted volumes and
// macOS firmlinked system-volume subtrees.
func platformPrune() []string {
	return []string{"/Volumes", "/System/Volumes"}
}
