//go:build !windows && !darwin && !linux

package main

// sysDiskHealth has no data source on this platform.
func sysDiskHealth() []DiskInfo {
	return nil
}