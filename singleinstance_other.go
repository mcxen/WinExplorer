//go:build !windows

package main

// The socket lock alone covers macOS and Linux.
func osInstanceLock() (bool, func()) { return true, nil }

func raiseRunningInstance() {}
