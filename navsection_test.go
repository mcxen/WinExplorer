package main

import (
	"os"
	"path/filepath"
	"testing"
)

// navsection_test.go drives the 快速访问 and 最近位置 headings' collapse:
// the twist hides the section's rows while the heading stays — a drop on
// 快速访问 still pins — and the keyboard's Left/Right folds and unfolds it.

// TestNavSectionCollapse: the 快速访问 twist hides the pinned rows, the
// heading stays on screen for a drop, and a second click brings them back.
func TestNavSectionCollapse(t *testing.T) {
	a, tt, places := navJumpApp(t)
	pinned := places["固定甲"]
	if navRowIndex(a, "nav-"+pinned) < 0 {
		t.Fatal("the pinned row is not in the pane")
	}
	if err := tt.Click("折叠 快速访问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if navRowIndex(a, "nav-"+pinned) >= 0 {
		t.Fatal("the collapsed section still lists its pins")
	}
	if navRowIndex(a, "nav-quick-access") < 0 {
		t.Fatal("the collapsed section lost its heading")
	}
	if a.navQuickExpanded {
		t.Fatal("navQuickExpanded still true")
	}
	if _, ok := tt.Find("快速访问"); !ok {
		t.Fatal("快速访问 heading is not on screen")
	}
	if err := tt.Click("折叠 快速访问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.navQuickExpanded || navRowIndex(a, "nav-"+pinned) < 0 {
		t.Fatal("the twist did not open the section back")
	}
}

// TestNavSectionKeys: with the cursor on a section heading, Left folds it
// and Right unfolds it, and the rows under a folded section are gone from
// the list the keys walk.
func TestNavSectionKeys(t *testing.T) {
	a, _, places := navJumpApp(t)
	rows := a.navRows()
	i := navRowIndex(a, "nav-quick-access")
	a.navTreeSetOpen(rows[i], false)
	if a.navQuickExpanded {
		t.Fatal("navTreeSetOpen left 快速访问 open")
	}
	if navRowIndex(a, "nav-"+places["固定甲"]) >= 0 {
		t.Fatal("the folded section still lists its pins")
	}
	rows = a.navRows()
	a.navTreeSetOpen(rows[navRowIndex(a, "nav-quick-access")], true)
	if !a.navQuickExpanded || navRowIndex(a, "nav-"+places["固定甲"]) < 0 {
		t.Fatal("navTreeSetOpen did not open 快速访问")
	}

	// Left on an open heading folds it; Left on a row under a folded
	// section's twin moves to the nearest heading above.
	i = navRowIndex(a, "nav-recent-head")
	rows = a.navRows()
	a.navTreeLeft(rows, i)
	if a.navRecentExpanded {
		t.Fatal("Left did not fold 最近位置")
	}
	a.navTreeRight(a.navRows(), navRowIndex(a, "nav-recent-head"))
	if !a.navRecentExpanded {
		t.Fatal("Right did not unfold 最近位置")
	}
	ri := navRowIndex(a, "nav-recent-"+places["最近乙"])
	a.navTreeLeft(a.navRows(), ri)
	if a.navTree.cursor != "nav-recent-head" {
		t.Fatalf("Left from a recent row went to %q", a.navTree.cursor)
	}

	// Enter on a heading folds it rather than navigating.
	a.navTreeActivate(a.navRows()[navRowIndex(a, "nav-recent-head")])
	if a.navRecentExpanded {
		t.Fatal("Enter did not fold 最近位置")
	}
}

// TestNavSectionPinDropWhileCollapsed: a drop on the folded 快速访问
// heading still pins, and the pin shows when the section opens again.
func TestNavSectionPinDropWhileCollapsed(t *testing.T) {
	a, _, dir := keynavWindow(t, 1)
	a.store.Pinned = nil
	a.navQuickExpanded = false
	note := filepath.Join(dir, "pin-me.txt")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.doPinDrop([]string{note})
	if !a.store.isPinned(note) {
		t.Fatal("the drop did not pin")
	}
	if navRowIndex(a, "nav-"+note) >= 0 {
		t.Fatal("the folded section lists the fresh pin")
	}
	a.navQuickExpanded = true
	if navRowIndex(a, "nav-"+note) < 0 {
		t.Fatal("the opened section does not list the pin")
	}
}
