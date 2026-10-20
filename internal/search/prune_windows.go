//go:build windows

package search

// platformPrune is empty because every Windows drive is already a search root.
func platformPrune() []string { return nil }
