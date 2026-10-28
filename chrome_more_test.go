package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCommandBarMoreMenu: 查看更多 lists Explorer's verbs in its groups,
// each with a glyph; plugin leaves stay under 插件.
func TestCommandBarMoreMenu(t *testing.T) {
	a := newStoreApp(t, t.TempDir())
	got := menuLabels(a.moreMenu())
	want := []string{"撤销", "-", "全选", "全部取消", "反向选择", "-", "插件", "-", "属性", "设置"}
	if !slices.Equal(got, want) {
		t.Fatalf("查看更多 = %v, want %v", got, want)
	}
	for _, e := range a.moreMenu() {
		if !e.sep && e.icon == nil {
			t.Fatalf("%s has no glyph", e.label)
		}
	}
	for _, p := range registeredPlugins() {
		a.pluginSetEnabled(p.ID, false)
	}
	if slices.Contains(menuLabels(a.moreMenu()), "插件") {
		t.Fatal("插件 shown with every plugin off")
	}
}

// TestCommandMenuFlyout: under 经典 the menu is drawn in the window and a
// row runs its verb and closes it; under v2 it is the system's.
func TestCommandMenuFlyout(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	a := newStoreApp(t, dir)
	t.Cleanup(func() { visualV2 = false; resolveTokens() })
	a.applySettings(func(x *appSettings) { x.VisualStyle = "" })
	a.reload()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("查看更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.Menu() != nil || !tt.HasText("反向选择") {
		t.Fatalf("flyout not drawn: menu=%v", tt.Menu())
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.cur().selected) != 2 || a.fly.open != "" {
		t.Fatalf("全选: selected=%d fly=%v", len(a.cur().selected), a.fly)
	}

	a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
	tt.Frame()
	if err := tt.Click("查看更多"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); !slices.Equal(got, menuLabels(a.moreMenu())) {
		t.Fatalf("v2 system menu = %v", got)
	}
}
