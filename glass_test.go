package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestFloatDialogsRender boots the real window view and opens every floating
// dialog in turn, letting the entrance motion run its course: the settings
// pages, properties, batch rename, Quick Look, the duplicate finder, the usage
// treemap, the extension editor and the search palette. It guards the adoption
// of components.Dialog: a body that fails to lay out, a width that leaves the
// glass, or a transition that never settles fails here. (The .DS_Store
// cleaner's dialog is the same shape and dsstore_test.go opens it.)
func TestFloatDialogsRender(t *testing.T) {
	a, tt, dir := keynavWindow(t, 4)

	openings := []struct {
		name string
		open func()
	}{
		{"设置", func() { a.settingsOpen = true }},
		{"属性", func() {
			a.selectOnly(dir + "/f000.txt")
			a.showProperties()
		}},
		{"批量重命名", func() {
			a.selectOnly(dir + "/f000.txt")
			a.batch.open = true
		}},
		{"快速预览", func() { a.quickLook = true }},
		{"重复文件查找", func() { *a.dupsOpenFlag() = true }},
		{"可视化查看占用", func() { *a.usageOpenFlag() = true }},
		{"扩展编辑", func() { a.extEdit.open = true }},
		{"快速搜索", func() { a.searchBox.open = true }},
	}
	for _, o := range openings {
		o.open()
		tt.Advance(300 * time.Millisecond) // the entrance motion finishes
	}
}

// TestFloatDialogFitsItsBody checks the panel is as wide as the body it was
// asked for plus the padding components.Dialog gives every panel: MyGo does
// not clamp a child to its parent's content box, so a body wider than the glass
// would hang outside it (which is what the fixed panel width did before
// floatDialog added the padding back).
func TestFloatDialogFitsItsBody(t *testing.T) {
	open := true
	const body = 580
	var bodyW, bodyLeft, bodyRight, pad float32
	view := func(c *ui.Context) {
		// The padding components.Dialog gives the panel, as floatDialog reads it.
		pad = c.Theme().Space(5)
		floatDialog(c, &open, "probe", body, false, func() {
			col := ui.Column(c).MinWidth(body)
			col.Draw(func(_ *ui.Painter, r ui.Rect) {
				bodyW, bodyLeft, bodyRight = r.W, r.X, r.X+r.W
			})
			col.Children(func() { ui.Text(c, "x") })
		})
	}
	tt := ui.NewTester(view, 1200, 800)
	panel, ok := tt.Find("probe")
	if !ok {
		t.Fatal("the panel was not laid out")
	}

	if want := float32(body) + 2*pad; panel.W != want {
		t.Errorf("panel width = %.1f, want %.1f (body %d + 2×%.0f padding)", panel.W, want, body, pad)
	}
	if bodyW != body {
		t.Errorf("body width = %.1f, want %d", bodyW, body)
	}
	if bodyLeft < panel.X || bodyRight > panel.X+panel.W {
		t.Errorf("body [%.1f, %.1f] hangs outside the panel [%.1f, %.1f]",
			bodyLeft, bodyRight, panel.X, panel.X+panel.W)
	}
}
