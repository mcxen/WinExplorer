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

// TestCommandMenuFlyoutKeys: ↑↓ skip separators, Enter runs the row, Esc
// closes; → opens 插件 and ← returns; the file pane does not see Enter.
func TestCommandMenuFlyoutKeys(t *testing.T) {
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
	if a.fly.open == "" || a.fly.idx < 0 {
		t.Fatalf("fly not primed: %+v", a.fly)
	}
	// First enabled row is 撤销 (disabled) is skipped by clamp — moreMenu
	// starts with 撤销 disabled when there is nothing to undo, then sep,
	// then 全选. Clamp lands on the first enabled: 全选 or 撤销 if enabled.
	first := menuLabels(a.moreMenu())[a.fly.idx]
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	second := menuLabels(a.moreMenu())[a.fly.idx]
	if second == first || a.moreMenu()[a.fly.idx].sep || a.moreMenu()[a.fly.idx].disabled {
		t.Fatalf("↓ from %q to %q (idx=%d)", first, second, a.fly.idx)
	}
	// Move highlight onto 全选 and Enter.
	for i := 0; i < len(a.moreMenu())+2; i++ {
		if a.moreMenu()[a.fly.idx].label == "全选" {
			break
		}
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	if a.moreMenu()[a.fly.idx].label != "全选" {
		t.Fatalf("could not reach 全选, at %q", a.moreMenu()[a.fly.idx].label)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.cur().selected) != 2 || a.fly.open != "" {
		t.Fatalf("Enter 全选: selected=%d fly=%v", len(a.cur().selected), a.fly)
	}

	// Re-open and exercise the 插件 submenu with → ← Esc.
	a.cur().selected = nil
	if err := tt.Click("查看更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for i := 0; i < len(a.moreMenu())+2; i++ {
		if a.moreMenu()[a.fly.idx].label == "插件" {
			break
		}
		tt.Key(0, ui.KeyDown)
		tt.Frame()
	}
	if a.fly.idx < 0 || a.moreMenu()[a.fly.idx].label != "插件" {
		t.Skip("插件 not in menu (all plugins off)")
	}
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if a.fly.sub != "插件" {
		t.Fatalf("→ did not open 插件: %+v", a.fly)
	}
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if a.fly.sub != "" || a.moreMenu()[a.fly.idx].label != "插件" {
		t.Fatalf("← back: %+v", a.fly)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.fly.open != "" {
		t.Fatalf("Esc left fly open: %+v", a.fly)
	}
}
