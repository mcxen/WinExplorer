//go:build !linux

package search

// networkMounts is Linux-only; elsewhere network shares are separate roots
// that the caller chooses.
func networkMounts() []string { return nil }
