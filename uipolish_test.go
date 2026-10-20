package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTileName(t *testing.T) {
	got := tileName("IMG_20261012_183344.jpg", 44)
	if strings.ReplaceAll(got, "\u200b", "") != "IMG_20261012_183344.jpg" {
		t.Fatalf("short name changed: %q", got)
	}
	if !strings.Contains(got, "_\u200b2026") || !strings.Contains(got, ".\u200bjpg") {
		t.Fatalf("no break opportunities after separators: %q", got)
	}
	long := tileName("一个非常非常长的文件名称用来测试中间省略号是否正确显示的效果-final.docx", 26)
	plain := strings.ReplaceAll(long, "\u200b", "")
	if n := len([]rune(plain)); n != 26 || !strings.Contains(plain, "…") || !strings.HasSuffix(plain, "final.docx") {
		t.Fatalf("middle ellipsis = %q (%d runes)", plain, n)
	}
}

func TestSqueezeCols(t *testing.T) {
	cases := []struct {
		over, date, typ float32
		free            bool
		wantD, wantT    float32
	}{
		{-10, colDateW, colTypeW, true, colDateW, colTypeW},
		{30, colDateW, colTypeW, true, colDateW - 30, colTypeW},
		{80, colDateW, colTypeW, true, colDateFitMin, colTypeW - (80 - (colDateW - colDateFitMin))},
		{1000, colDateW, colTypeW, true, colDateFitMin, colTypeFitMin},
		{100, 150, 140, false, 150, 140}, // dragged widths are the user's
	}
	for _, c := range cases {
		d, ty := squeezeCols(c.over, c.date, c.typ, c.free, c.free)
		if d != c.wantD || ty != c.wantT {
			t.Errorf("squeezeCols(%v) = %v, %v; want %v, %v", c.over, d, ty, c.wantD, c.wantT)
		}
	}
}

func TestTabTitleAndKnownFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dl := filepath.Join(home, "Downloads")
	os.MkdirAll(filepath.Join(dl, "sub"), 0o755)
	for path, want := range map[string]string{
		pathHome: "主文件夹", pathGallery: "画廊", pathTrash: "回收站",
		pathThisPC: "此电脑", dl: "下载", filepath.Join(dl, "sub"): "sub", home: "主文件夹",
	} {
		if got := tabTitle(path); got != want {
			t.Errorf("tabTitle(%q) = %q, want %q", path, got, want)
		}
	}
	if !underKnownFolder(filepath.Join(dl, "sub")) || !underKnownFolder(home) {
		t.Error("known folders should not open the tree")
	}
	if underKnownFolder(filepath.Join(home, "Projects")) || underKnownFolder("/etc") {
		t.Error("other folders still open the tree")
	}
}
