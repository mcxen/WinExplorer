package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestTabHoverSwitch keeps the delayed tab switch's boundaries explicit:
// the timer starts only for another tab, waits 600ms, and ignores a zero
// target as the frame's "leave" marker.
func TestTabHoverSwitch(t *testing.T) {
	since := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		prevID int
		id     int
		after  time.Duration
		want   bool
	}{
		{"before the delay", 1, 2, 599 * time.Millisecond, false},
		{"at the delay", 1, 2, 600 * time.Millisecond, true},
		{"after the delay", 1, 2, 900 * time.Millisecond, true},
		{"front tab stays", 2, 2, 900 * time.Millisecond, false},
		{"left the tab strip", 1, 0, 900 * time.Millisecond, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tabHoverSwitch(tt.prevID, tt.id, since, since.Add(tt.after)); got != tt.want {
				t.Fatalf("tabHoverSwitch(%d, %d, +%v) = %v, want %v",
					tt.prevID, tt.id, tt.after, got, tt.want)
			}
		})
	}
}

// tabsWindow boots the real window over a root holding the folders named,
// each with one file <name>.txt, and returns the app, the tester and root.
func tabsWindow(t *testing.T, w int, folders ...string) (*App, *ui.Tester, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range folders {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d, d+".txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, root)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, w, 800)
	tt.Frame()
	return a, tt, root
}

func tabPaths(a *App) []string {
	var out []string
	for _, t := range a.tabs {
		out = append(out, filepath.Base(t.path))
	}
	return out
}

// TestRestoredTabListsWhenSwitchedTo: a restored session reads only the
// front tab's folder, so the others came forward empty until 刷新.
func TestRestoredTabListsWhenSwitchedTo(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
		os.WriteFile(filepath.Join(root, d, "f.txt"), nil, 0o644)
	}
	store := &persistedState{Pinned: defaultPins()}
	a := newSessionApp(store, windowSession{
		Tabs: []string{filepath.Join(root, "a"), filepath.Join(root, "b")}, Active: 1,
	}, 0)
	a.switchTo(0)
	if len(a.cur().entries) != 1 {
		t.Fatalf("restored back tab lists %d entries, want 1", len(a.cur().entries))
	}
	// A file added to a back tab's folder shows when it comes forward.
	os.WriteFile(filepath.Join(root, "b", "g.txt"), nil, 0o644)
	a.switchTo(1)
	if len(a.cur().entries) != 2 {
		t.Fatalf("back tab came forward with %d entries, want the fresh 2", len(a.cur().entries))
	}
}

// TestTabShortcuts drives every tab key through the window.
func TestTabShortcuts(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b", "c")
	a.navigate(filepath.Join(root, "a"))
	tt.Key(ui.Ctrl, ui.KeyT)
	if len(a.tabs) != 2 || a.activeTab != 1 || a.cur().path != filepath.Join(root, "a") {
		t.Fatalf("Ctrl+T: tabs %v active %d", tabPaths(a), a.activeTab)
	}
	a.navigate(filepath.Join(root, "b"))
	a.navigate(filepath.Join(root, "c"))
	a.openTab(root)
	a.switchTo(1)
	tt.Key(ui.Ctrl, ui.KeyW)
	if got := tabPaths(a); !slices.Equal(got, []string{"a", filepath.Base(root)}) || a.activeTab != 1 {
		t.Fatalf("Ctrl+W on the middle tab: %v active %d, want the right neighbour in front", got, a.activeTab)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyT)
	if got := tabPaths(a); !slices.Equal(got, []string{"a", "c", filepath.Base(root)}) || a.activeTab != 1 {
		t.Fatalf("Ctrl+Shift+T: %v active %d, want c back in its place", got, a.activeTab)
	}
	if !a.canBack() {
		t.Fatal("the reopened tab lost its history")
	}
	tt.Key(ui.Alt, ui.KeyLeft)
	if filepath.Base(a.cur().path) != "b" {
		t.Fatalf("后退 in the reopened tab went to %q, want b", a.cur().path)
	}

	steps := []struct {
		name string
		mods ui.Modifiers
		key  ui.Key
		want int
	}{
		{"Ctrl+Tab", ui.Ctrl, ui.KeyTab, 2},
		{"Ctrl+Tab wraps", ui.Ctrl, ui.KeyTab, 0},
		{"Ctrl+Shift+Tab wraps", ui.Ctrl | ui.Shift, ui.KeyTab, 2},
		{"Ctrl+PageUp", ui.Ctrl, ui.KeyPageUp, 1},
		{"Ctrl+PageDown", ui.Ctrl, ui.KeyPageDown, 2},
		{"Ctrl+1", ui.Ctrl, ui.Key1, 0},
		{"Ctrl+9 is the last", ui.Ctrl, ui.Key9, 2},
		{"Ctrl+5 past the end stays", ui.Ctrl, ui.Key5, 2},
		{"Cmd+Shift+[", ui.Cmd | ui.Shift, ui.KeyBracketLeft, 1},
		{"Cmd+Shift+]", ui.Cmd | ui.Shift, ui.KeyBracketRight, 2},
	}
	for _, s := range steps {
		tt.Key(s.mods, s.key)
		if a.activeTab != s.want {
			t.Fatalf("%s: active %d, want %d", s.name, a.activeTab, s.want)
		}
	}
	// Cmd+] stays 前进, not a tab switch.
	a.switchTo(1)
	a.back()
	tt.Key(ui.Cmd, ui.KeyBracketRight)
	if a.activeTab != 1 || filepath.Base(a.cur().path) != "b" {
		t.Fatalf("Cmd+]: active %d path %q, want 前进 in tab 1", a.activeTab, a.cur().path)
	}
	// The tab keys work from the address bar's edit, which they drop.
	a.beginPathEdit()
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyTab)
	if a.activeTab != 2 || a.tabs[1].editingPath {
		t.Fatalf("Ctrl+Tab from the address bar: active %d, editing left %v", a.activeTab, a.tabs[1].editingPath)
	}
	tt.Key(ui.Ctrl, ui.KeyF4)
	if len(a.tabs) != 2 || a.activeTab != 1 {
		t.Fatalf("Ctrl+F4 on the last tab: %v active %d, want the left neighbour", tabPaths(a), a.activeTab)
	}
}

// TestCloseManyTabs covers the tab menu's bulk closes and the reopen stack.
func TestCloseManyTabs(t *testing.T) {
	a, _, root := tabsWindow(t, 1200, "a", "b", "c", "d")
	a.navigate(filepath.Join(root, "a"))
	for _, d := range []string{"b", "c", "d"} {
		a.openTab(filepath.Join(root, d))
	}
	a.switchTo(3)
	a.closeOthers(1)
	if got := tabPaths(a); !slices.Equal(got, []string{"b"}) || len(a.cur().entries) != 1 {
		t.Fatalf("关闭其他标签页 from a back tab: %v, entries %d", got, len(a.cur().entries))
	}
	for range 3 {
		a.reopenClosedTab()
	}
	if got := tabPaths(a); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("reopening all: %v, want the original order", got)
	}
	a.switchTo(3)
	a.closeRight(0)
	if got := tabPaths(a); !slices.Equal(got, []string{"a"}) || a.activeTab != 0 {
		t.Fatalf("关闭右侧标签页 over the front tab: %v active %d", got, a.activeTab)
	}
	a.closeTab(0)
	if len(a.tabs) != 1 {
		t.Fatal("the last tab closed")
	}
}

// TestTabStateStaysPerTab: what one tab holds — selection, query, history,
// a half-done rename — is not seen from another.
func TestTabStateStaysPerTab(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b")
	a.navigate(filepath.Join(root, "a"))
	a.selectOnly(filepath.Join(root, "a", "a.txt"))
	a.doRenameStart()
	a.duplicateTab(0)
	tt.Frame()
	if a.activeTab != 1 || len(a.cur().selected) != 0 || a.cur().renaming != "" {
		t.Fatalf("复制标签页 carried state: active %d sel %v renaming %q", a.activeTab, a.cur().selected, a.cur().renaming)
	}
	if a.tabs[0].renaming != "" {
		t.Fatal("the rename stayed open in the tab sent back")
	}
	if !a.canBack() {
		t.Fatal("复制标签页 dropped the history")
	}
	a.navigate(filepath.Join(root, "b"))
	tt.Frame()
	if !tt.HasText("b") {
		t.Fatalf("the tab title did not follow the navigation: %v", tt.Texts())
	}
	a.switchTo(0)
	if filepath.Base(a.cur().path) != "a" || len(a.cur().selected) != 1 || a.canForward() {
		t.Fatalf("tab 0 changed: path %q sel %v", a.cur().path, a.cur().selected)
	}
	// A selected file deleted meanwhile is not selected out of sight.
	os.Remove(filepath.Join(root, "a", "a.txt"))
	a.switchTo(1)
	a.switchTo(0)
	if len(a.cur().selected) != 0 {
		t.Fatalf("a deleted file stayed selected: %v", a.cur().selected)
	}
}

// TestTabStripOverflow: many tabs shrink to the minimum and then scroll,
// the caption buttons stay on screen, and the front tab stays in view.
func TestTabStripOverflow(t *testing.T) {
	a, tt, root := tabsWindow(t, 1000, "a", "zeta")
	for i := range 30 {
		a.openTab(filepath.Join(root, "a"))
		if i == 2 {
			tt.Frame()
			maxX := float32(4*(tabMaxW+2) + 20)
			if visualV2 {
				maxX += tabChromeMacW - tabDragMinW // traffic lights + plus footprint, no right captions
			}
			if r, ok := tt.Find("新建标签页"); !ok || r.X > maxX {
				t.Fatalf("few tabs: 新建标签页 at %v, want after four full tabs (maxX=%v)", r, maxX)
			}
		}
	}
	a.openTab(filepath.Join(root, "zeta"))
	tt.Frame()
	tt.Frame()
	if r, ok := tt.Find("关闭"); !ok || r.X+r.W > 1000 || r.W == 0 {
		t.Fatalf("the close caption button is off screen: %v", r)
	}
	r, ok := tt.Find("zeta")
	if !ok || r.X < 0 || r.X+r.W > 1000 || r.W == 0 {
		t.Fatalf("the front tab is out of view: %v %v", r, ok)
	}
	if w := tabWidth(1000, 32); w != tabMinW {
		t.Fatalf("32 tabs are %v wide, want the minimum", w)
	}
	rightReserve := float32(3 * 46)
	if visualV2 {
		rightReserve = tabDragMinW
	}
	if r, ok := tt.Find("新建标签页"); !ok || r.X+r.W > 1000-rightReserve || r.W == 0 {
		t.Fatalf("新建标签页 scrolled out of reach: %v", r)
	}
	// Ctrl+1 brings the first tab forward and scrolls it into view.
	tt.Key(ui.Ctrl, ui.Key1)
	tt.Frame()
	tt.Frame()
	if r, ok := tt.Find("zeta"); ok && r.X < 1000-rightReserve-tabDragMinW && r.W > 0 {
		t.Fatalf("the strip did not scroll back: zeta still at %v", r)
	}
}

// TestTabMouse: dragging reorders, the middle button closes, and a double
// click on the strip's empty space opens a tab.
func TestTabMouse(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b", "c")
	a.navigate(filepath.Join(root, "a"))
	a.openTab(filepath.Join(root, "b"))
	a.openTab(filepath.Join(root, "c"))
	a.switchTo(1)
	tt.Frame()
	r, ok := tt.Find("b")
	if !ok {
		t.Fatalf("no tab b: %v", tt.Texts())
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	for dx := float32(0); dx <= 260; dx += 20 {
		tt.Move(x+dx, y)
		tt.Frame()
	}
	tt.Release(x+260, y)
	tt.Frame()
	if got := tabPaths(a); !slices.Equal(got, []string{"a", "c", "b"}) || filepath.Base(a.cur().path) != "b" {
		t.Fatalf("drag right: %v front %q, want b moved past c and in front", got, a.cur().path)
	}

	id := a.tabs[0].id
	a.tabPointer(id, ui.InputEvent{Kind: ui.InputPointerDown, Button: 2})
	a.tabPointer(id, ui.InputEvent{Kind: ui.InputPointerUp, Button: 2})
	if got := tabPaths(a); !slices.Equal(got, []string{"c", "b"}) || filepath.Base(a.cur().path) != "b" {
		t.Fatalf("middle click on a back tab: %v front %q", got, a.cur().path)
	}
	if a.tabPointer(a.tabs[0].id, ui.InputEvent{Kind: ui.InputPointerUp, Button: 0}) {
		t.Fatal("the primary button was taken from the tab's click")
	}

	tt.Frame()
	p, ok := tt.Find("新建标签页")
	if !ok {
		t.Fatal("no 新建标签页 button")
	}
	// The strip's tail, just left of 新建标签页.
	tt.ClickAt(p.X-10, p.Y+p.H/2)
	tt.ClickAt(p.X-10, p.Y+p.H/2)
	tt.Frame()
	if len(a.tabs) != 3 || a.activeTab != 2 {
		t.Fatalf("double click on the empty strip: %d tabs, active %d", len(a.tabs), a.activeTab)
	}
}
