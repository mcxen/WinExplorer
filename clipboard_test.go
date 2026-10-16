package main

import (
	"slices"
	"testing"
	"time"

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
	a.sysClipAt = time.Now()
	if !a.canPaste() {
		t.Fatal("cached system clipboard should paste")
	}
}
