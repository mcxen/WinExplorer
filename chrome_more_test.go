package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// moreMenuMaxTopItems / moreMenuMaxSeps are the budget for the command bar's
// 查看更多 flyout (docs/specs/16-commands-layout.md): stay near Windows 11
// Explorer's ~6–7 items and at most 3 separator groups.
const (
	moreMenuMaxTopItems = 8
	moreMenuMaxSeps     = 3
)

func countMenuBudget(labels []string) (items, seps int) {
	for _, s := range labels {
		if s == "-" {
			seps++
		} else {
			items++
		}
	}
	return
}

// TestCommandBarMoreMenu: with the default-enabled plugins, 查看更多 lists
// undo, selection verbs, a 插件 submenu, then properties and settings —
// plugin leaves stay nested so the top level stays under the item budget.
func TestCommandBarMoreMenu(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("查看更多"); err != nil {
		t.Fatal(err)
	}
	got := tt.Menu()
	want := []string{"撤销", "-", "全选", "全部取消", "-", "插件", "-", "属性", "设置"}
	if !slices.Equal(got, want) {
		t.Fatalf("查看更多 = %v, want %v", got, want)
	}
	items, seps := countMenuBudget(got)
	if items > moreMenuMaxTopItems {
		t.Fatalf("top-level items = %d, budget %d", items, moreMenuMaxTopItems)
	}
	if seps > moreMenuMaxSeps {
		t.Fatalf("separators = %d, budget %d", seps, moreMenuMaxSeps)
	}
	tt.CloseMenu()

	// Nested plugin leaves still come from MoreItems (reachable under 插件).
	var labels []string
	for _, it := range a.pluginMoreItems() {
		labels = append(labels, it.Label)
	}
	if !slices.Equal(labels, []string{"远程访问", "复制远程地址"}) {
		t.Fatalf("插件 submenu source = %v", labels)
	}
}

// TestCommandBarMoreMenuNoPlugins: with every plugin off, 查看更多 drops the
// 插件 submenu and still stays inside the item budget.
func TestCommandBarMoreMenuNoPlugins(t *testing.T) {
	dir := t.TempDir()
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	for _, p := range registeredPlugins() {
		a.pluginSetEnabled(p.ID, false)
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("查看更多"); err != nil {
		t.Fatal(err)
	}
	got := tt.Menu()
	want := []string{"撤销", "-", "全选", "全部取消", "-", "属性", "设置"}
	if !slices.Equal(got, want) {
		t.Fatalf("查看更多 (no plugins) = %v, want %v", got, want)
	}
	items, seps := countMenuBudget(got)
	if items > moreMenuMaxTopItems || seps > moreMenuMaxSeps {
		t.Fatalf("budget broken: items=%d seps=%d", items, seps)
	}
}
