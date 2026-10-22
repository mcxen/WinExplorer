//go:build windows

package diskhealth

import (
	"context"
	"testing"
)

// TestQuietCommandHidesConsole pins the flag the tray's disk poll needs.
// Without it, each powershell.exe probe opens its own console because the
// app is linked with -H=windowsgui.
func TestQuietCommandHidesConsole(t *testing.T) {
	cmd := quietCommand(context.Background(), "powershell", "-NoProfile", "-Command", "exit 0")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("SysProcAttr = %#v, want HideWindow and CREATE_NO_WINDOW", cmd.SysProcAttr)
	}
}
