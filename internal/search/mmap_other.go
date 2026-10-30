//go:build !unix

package search

import "os"

// mapFile reads path into one slice where mmap is not wired up (Windows,
// wasm). The rows are still fixed-width views, not per-entry objects.
func mapFile(path string) ([]byte, func(), error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return data, func() {}, nil
}
