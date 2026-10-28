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

// 默认不自动展开（像 Windows 资源管理器）：从没设置过的用户导航不打开祖先。
func TestNavTreeFollowOffByDefault(t *testing.T) {
	var s appSettings
	if s.treeExpand() {
		t.Fatal("follow must default to off")
	}
	on := true
	s.TreeExpand = &on
	if !s.treeExpand() {
		t.Fatal("explicit on must stay on")
	}
}

// 开启跟随时只保留到当前目录的祖先链：换到别处后，上一次自动展开的
// 祖先收起，用户手动展开的保留。
func TestNavTreeFollowKeepsOnlyCurrentChain(t *testing.T) {
	a, _, dir := navTreeApp(t)
	one := filepath.Join(dir, "a-sub", "x", "deep")
	two := filepath.Join(dir, "B-sub", "y")
	for _, p := range []string{one, two} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.navigate(one)
	a.navTreeFollow()
	if !a.navTree.open[filepath.Join(dir, "a-sub", "x")] {
		t.Fatalf("chain not opened: %v", a.navTree.open)
	}
	a.navigate(two)
	a.navTreeFollow()
	if a.navTree.open[filepath.Join(dir, "a-sub", "x")] || a.navTree.open[filepath.Join(dir, "a-sub")] {
		t.Fatalf("previous chain left open: %v", a.navTree.open)
	}
	if !a.navTree.open[filepath.Join(dir, "B-sub")] || !a.navTree.open[dir] {
		t.Fatalf("new chain not open: %v", a.navTree.open)
	}
}
