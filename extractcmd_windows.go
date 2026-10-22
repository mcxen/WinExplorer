//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// extractcmd_windows.go: a GUI app must not flash a console for every
// spawned extractor. runExtract routes every command through hideConsole.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
