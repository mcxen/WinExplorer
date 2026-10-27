package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestQuickLookBarLayout: the quick preview's bar carries 打开 with its
// 打开方式 half, 更多 and 关闭, and 更多 lists locate/copy, share and
// properties in that grouping (docs/specs/16).
func TestQuickLookBarLayout(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("hello"), 0o644)
	}
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.selectOnly(filepath.Join(dir, "a.txt"))
	a.openQuickLook()
	for i := 0; i < 40; i++ {
		tt.Frame()
		if st := a.preview.Load(); st != nil && st.kind != previewNone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	tt.Frame()
	if !tt.HasText("a.txt") || !tt.HasText("打开") {
		t.Fatalf("bar texts: %v", tt.Texts())
	}
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	want := []string{"在文件夹中显示", "复制路径", "复制", "-", "分享", "-", "属性"}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Fatalf("更多 = %v, want %v", got, want)
	}
	tt.CloseMenu()
	if err := tt.Click("关闭 (Esc)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.quickLook {
		t.Fatal("关闭 left the preview open")
	}
}
