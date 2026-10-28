package main

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/transfer"
)

// The system-clipboard interop rides on transfer.FileData/Data.Files:
// lock their round trip — spaces, Unicode, absolute paths only.
func TestFileClipboardRoundTrip(t *testing.T) {
	in := []string{"/Users/x/a b c.txt", "/Users/x/项目/图片.heic", "/Volumes/USB/disk"}
	data, err := transfer.FileData(in...)
	if err != nil {
		t.Fatalf("FileData: %v", err)
	}
	out, err := data.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if !slices.Equal(out, in) {
		t.Fatalf("round trip = %v want %v", out, in)
	}

	// Relative paths are refused; the app only ever offers absolutes.
	if _, err := transfer.FileData("relative/path.txt"); err == nil {
		t.Fatal("relative path accepted")
	}
}

// canPaste falls back to the system clipboard when the app's own is
// empty; before the app is ready the system read is skipped safely.
func TestCanPasteGuards(t *testing.T) {
	a := &App{}
	if a.canPaste() {
		t.Fatal("empty app should not paste")
	}
	a.clipPaths = []string{"/x"}
	if !a.canPaste() {
		t.Fatal("internal clipboard should paste")
	}
	a.clipPaths = nil
	a.sysClip = []string{"/Users/x/f.txt"}
	if !a.canPaste() {
		t.Fatal("cached system clipboard should paste")
	}
}

// 视图每帧都问 canPaste：它只能读缓存，绝不能去读系统剪贴板——X11 上那是
// 一次会卡住主循环的往返（v0.3.5 窗口不重绘、输入被吞的根因）。
func TestCanPasteNeverReadsSystemClipboard(t *testing.T) {
	reads := 0
	old := readSystemClipboardFiles
	readSystemClipboardFiles = func() ([]string, error) { reads++; return []string{"/f"}, nil }
	defer func() { readSystemClipboardFiles = old }()
	a := &App{}
	for range 100 {
		a.canPaste()
	}
	if reads != 0 {
		t.Fatalf("canPaste read the system clipboard %d times", reads)
	}
	if got := a.refreshSystemClipboard(); len(got) != 1 || !a.canPaste() || reads != 1 {
		t.Fatalf("refresh = %v, reads = %d", got, reads)
	}
}
