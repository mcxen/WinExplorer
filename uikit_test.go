package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// uikit_test.go pins the dialog buttons' metrics: the label sits inside the
// button with btnPadX on each side, and a short label is held to btnMinW so the
// buttons of a row come out one width — the widest label plus the padding — as
// Windows dialogs size them. A button that hugs its own text reads as a label
// with a background, and a pair of them looks ragged.

// labelWidth renders one label alone in a tester of its own and returns its
// box, so a test can tell how much room a button's padding should add. The row
// is what gives it an intrinsic width: a text element alone in the root takes
// the window's.
func labelWidth(t *testing.T, label string, font float32) ui.Rect {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Shrink(0).Children(func() {
			ui.Text(c, label).FontSize(font).SingleLine()
		})
	}, 400, 100)
	tt.Frame()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("the label %q did not render alone", label)
	}
	return r
}

// TestUsageDialogButtonsCarryTheirPadding measures the 可视化查看占用 dialog's
// buttons — the ones that hugged their text — against their own labels.
func TestUsageDialogButtonsCarryTheirPadding(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	tree := &usageNode{Name: "root", Path: dir, IsDir: true, Size: 10, Children: []*usageNode{
		{Name: "big.tar", Path: dir + "/big.tar", Size: 6},
		{Name: "small.bin", Path: dir + "/small.bin", Size: 4},
	}}
	usageResult.Store(&usageScan{root: dir, tree: tree, files: 2, dirs: 1})
	*a.usageOpenFlag() = true
	tt.Frame()

	for _, label := range []string{"在窗口中打开", "重新扫描"} {
		button, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no button shows %q: %v", label, tt.Texts())
		}
		text := labelWidth(t, label, 12)
		if button.W < text.W+2*btnPadX-1 {
			t.Fatalf("%q's button is %v wide for a %v label: the text is not inset",
				label, button.W, text.W)
		}
		if button.W < btnMinW {
			t.Fatalf("%q's button is %v wide, under the %v floor", label, button.W, btnMinW)
		}
	}
	// A label shorter than the floor is held to exactly it: the floor is what
	// makes 重新扫描 and 关闭 — the pair the dialog shows — one width.
	short, _ := tt.Find("重新扫描")
	if short.W != btnMinW {
		t.Fatalf("重新扫描's button is %v wide, want the %v floor", short.W, btnMinW)
	}
}
