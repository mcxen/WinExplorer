//go:build !darwin && !windows && !linux

package search

// platformPrune is empty on platforms whose roots already cover the walk.
func platformPrune() []string { return nil }
