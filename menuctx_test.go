package main

import (
	"os"
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

// TestNativePaneMenu: a right-click on the pane's empty space opens the
// system menu (not the flyout), its 新建 submenu lists newFileKinds, and
// choosing 文本文档 creates the file.
func TestNativePaneMenu(t *testing.T) {
	root := t.TempDir()
	a := newStoreApp(t, root)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.RightClickAt(400, 500)
	if a.fly.open != "" {
		t.Fatalf("right-click opened the flyout %q", a.fly.open)
	}
	if m := tt.Menu(); len(m) == 0 || m[0] != "新建" {
		t.Fatalf("pane menu = %v", m)
	}
	if err := tt.ChooseMenuItem("新建", newFileKinds[0].label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	ents, _ := os.ReadDir(root)
	if len(ents) != 1 {
		t.Fatalf("after 新建 %s: %v", newFileKinds[0].label, ents)
	}
}
