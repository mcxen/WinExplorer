package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNavTreeNoExpand: with 展开到打开的文件夹 off navigating opens no
// ancestors, keeps what the user opened, and marks the nearest shown
// ancestor instead of the hidden current row.
func TestNavTreeNoExpand(t *testing.T) {
	a, _, dir := navTreeApp(t)
	a.applySettings(func(x *appSettings) { x.TreeNoExpand = true })
	deep := filepath.Join(dir, "a-sub", "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	a.navigate(deep)
	a.navTreeFollow()
	if a.navTree.open[filepath.Join(dir, "a-sub")] || a.navTree.reveal != "" {
		t.Fatalf("opened %v, reveal %q", a.navTree.open, a.navTree.reveal)
	}
	rows := a.navRows()
	if a.navTree.row >= 0 {
		t.Fatalf("hidden current folder has a row %d", a.navTree.row)
	}
	marked := 0
	for _, r := range rows {
		if r.under {
			marked++
		}
	}
	if marked != 1 {
		t.Fatalf("%d ancestor marks", marked)
	}
}
