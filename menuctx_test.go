package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// flyLabels lists the open flyout's rows ("-" for a separator), or nil.
func flyLabels(tt *ui.Tester, a *App) []string {
	tt.Frame()
	if a.fly.open == "" {
		return nil
	}
	return menuLabels(a.flyRoot)
}

// flyChoose runs the open flyout's row at path (a submenu's label first)
// in the next frame, as a click on it does.
func flyChoose(t *testing.T, tt *ui.Tester, a *App, path ...string) {
	t.Helper()
	tt.Frame()
	if e := flyFind(a.flyRoot, path); e == nil || e.disabled {
		t.Fatalf("flyout has no enabled %v in %v", path, menuLabels(a.flyRoot))
	}
	a.flyPick = path
	tt.Frame()
	tt.Frame()
}

func flyClose(tt *ui.Tester, a *App) {
	a.fly = flyState{}
	tt.Frame()
}
