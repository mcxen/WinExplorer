//go:build !windows

package main

import "os/exec"

// hideConsole is a Windows-only concern; elsewhere spawned extractors have
// no console to flash.
func hideConsole(*exec.Cmd) {}
