package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The size engine's UI side: the 大小 column's cell for folders, the
// status bar's selection total, and the 刷新统计 menu items. The engine
// itself is sizes_test.go's; its init keeps the cache file off the user's
// config, and these tests seed the memory cache and drive the menus
// through a headless window the way keynav_ui_test.go's do.

// menuIndexOf is the place of label among the labels of the menu shown,
// or -1 when the menu holds no such item.
func menuIndexOf(labels []string, label string) int {
	for i, l := range labels {
		if l == label {
			return i
		}
	}
	return -1
}

// TestSizeColumnText checks the 大小 column's cell for every state a row
// can be in: a file's own size, a folder's cached tally — an empty folder
// reads blank, as Explorer's does — "…" while the engine is walking, and blank before it is
// asked for.
func TestSizeColumnText(t *testing.T) {
	cached := "/sizes-ui/cached"
	empty := "/sizes-ui/empty"
	busy := "/sizes-ui/busy"
	dirStatsMu.Lock()
	dirStatsMem[cached] = dirStat{Size: 1234}
	dirStatsMem[empty] = dirStat{}
	dirStatsPending[busy] = true
	dirStatsMu.Unlock()
	t.Cleanup(func() {
		dirStatsMu.Lock()
		delete(dirStatsMem, cached)
		delete(dirStatsMem, empty)
		delete(dirStatsPending, busy)
		dirStatsMu.Unlock()
	})

	cases := []struct {
		name string
		e    Entry
		want string
	}{
		{"file", Entry{Path: "/sizes-ui/a.txt", Size: 2048}, "2.0 KB"},
		{"empty file", Entry{Path: "/sizes-ui/b.txt"}, "0 B"},
		{"cached folder", Entry{Path: cached, IsDir: true}, "1.2 KB"},
		{"cached empty folder", Entry{Path: empty, IsDir: true}, ""},
		{"folder being walked", Entry{Path: busy, IsDir: true}, "…"},
		{"folder not yet walked", Entry{Path: "/sizes-ui/unknown", IsDir: true}, ""},
	}
	for _, tc := range cases {
		if got := sizeColumnText(tc.e); got != tc.want {
			t.Fatalf("%s: sizeColumnText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestSelectedBytes checks the status bar's selection total: a folder's
// cached size — what the engine leaves in e.Size — counts alongside file
// sizes, and a folder whose size is not known yet adds nothing.
func TestSelectedBytes(t *testing.T) {
	entries := []Entry{
		{Path: "/sizes-ui/a.txt", Size: 100},
		{Path: "/sizes-ui/dir", IsDir: true, Size: 150},
		{Path: "/sizes-ui/other", IsDir: true}, // no tally yet: Size 0
	}
	if got := selectedBytes(entries); got != 250 {
		t.Fatalf("selectedBytes = %d, want 250", got)
	}
	if got := selectedBytes(entries[2:]); got != 0 {
		t.Fatalf("selectedBytes of an unknown folder = %d, want 0", got)
	}
}

// TestRefreshStatsMenus drives the two 刷新统计 items through a laid-out
// window: the row menu's rewalks the one selected folder, the pane
// background's the current folder — both without a reload — and neither
// chooses where it does not apply.
func TestRefreshStatsMenus(t *testing.T) {
	warmTerminal() // main warms it off the UI thread; the menu then lists 在终端中打开
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	loose := filepath.Join(root, "loose.txt")
	writeTree(t, filepath.Join(proj, "a.txt"), 10)
	writeTree(t, loose, 5)

	a := newStoreApp(t, root)
	// The bare test app takes grouping's zero value, which is 分组依据>
	// 名称, and iconZoom's, which would size the icons to nothing.
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle after the listing")
	}
	if st, ok := lookupDirStat(proj); !ok || st.Size != 10 {
		t.Fatalf("proj stat = %+v ok=%v, want size 10", st, ok)
	}
	// reload pulls the walked tally into the row, as the engine's UI hop
	// does in a running window.
	a.reload()

	// The status bar counts a selected folder's cached size.
	a.selectOnly(proj)
	if left := a.statusLeft(); !strings.Contains(left, "10 B") {
		t.Fatalf("statusLeft = %q, want the selected folder's 10 B in it", left)
	}

	// The row menu of the one selected folder: 刷新统计 between
	// 在终端中打开 and the plugins' items, and choosing it rewalks the
	// subtree after a change deep inside it.
	if err := tt.RightClick("proj"); err != nil {
		t.Fatal(err)
	}
	menu := flyLabels(tt, a)
	i, j, k := menuIndexOf(menu, "刷新统计"), menuIndexOf(menu, "在终端中打开"),
		menuIndexOf(menu, "可视化查看占用")
	if i < 0 || j < 0 || k < 0 || !(j < i && i < k) {
		t.Fatalf("row menu = %v: 刷新统计 must sit between 在终端中打开 and 可视化查看占用", menu)
	}
	writeTree(t, filepath.Join(proj, "b.txt"), 40)
	flyChoose(t, tt, a, "刷新统计")
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle after the row-menu refresh")
	}
	if st, ok := lookupDirStat(proj); !ok || st.Size != 50 {
		t.Fatalf("proj after the row-menu refresh = %+v ok=%v, want size 50", st, ok)
	}

	// With a file selected alongside the folder there is no one folder to
	// rewalk: the item does not show at all (no greyed-out entry).
	a.selectOnly(loose)
	a.toggleSelect(proj)
	if err := tt.RightClick("proj"); err != nil {
		t.Fatal(err)
	}
	if menuIndexOf(flyLabels(tt, a), "刷新统计") >= 0 {
		t.Fatalf("row menu = %v: 刷新统计 shown with no single folder", flyLabels(tt, a))
	}
	flyClose(tt, a)

	// The pane background's item sits between 刷新 and 显示隐藏的项目,
	// and choosing it rewalks the current folder: root's own tally lands,
	// the subtree's dropped cache does not come back on its own.
	a.clearSelection()
	tt.Frame()
	r, ok := tt.Find("proj")
	if !ok {
		t.Fatal("the proj row is not built")
	}
	tt.RightClickAt(r.X+20, r.Y+150)
	menu = flyLabels(tt, a)
	i, j, k = menuIndexOf(menu, "刷新统计"), menuIndexOf(menu, "刷新"),
		menuIndexOf(menu, "显示隐藏的项目")
	if i < 0 || j < 0 || k < 0 || !(j < i && i < k) {
		t.Fatalf("pane menu = %v: 刷新统计 must sit between 刷新 and 显示隐藏的项目", menu)
	}
	writeTree(t, filepath.Join(root, "c.txt"), 100)
	flyChoose(t, tt, a, "刷新统计")
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle after the pane-menu refresh")
	}
	// a.txt 10 + b.txt 40 + loose.txt 5 + c.txt 100.
	if st, ok := lookupDirStat(root); !ok || st.Size != 155 {
		t.Fatalf("root after the pane-menu refresh = %+v ok=%v, want size 155", st, ok)
	}
	if _, ok := lookupDirStat(proj); ok {
		t.Fatal("proj's cache survived the pane-menu refresh of its parent")
	}

	// On the 主文件夹 page no folder stands behind the pane. Pinning proj keeps the page's sections
	// built even on an account without the default folders. The item does
	// not show there at all.
	a.store.pin(proj)
	a.navigate(pathHome)
	tt.Frame()
	m, ok := tt.Find("尚无最近使用的文件")
	if !ok {
		t.Fatal("the 主文件夹 page's empty recents section is not built")
	}
	tt.RightClickAt(m.X+m.W/2, m.Y+m.H/2)
	menu = flyLabels(tt, a)
	if menuIndexOf(menu, "刷新统计") >= 0 || menuIndexOf(menu, "在终端中打开") >= 0 {
		t.Fatalf("pane menu = %v: folder-only items shown on the 主文件夹 page", menu)
	}
	flyClose(tt, a)
}
