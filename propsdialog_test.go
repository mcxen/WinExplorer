package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The 属性 dialog gates on the single selection: nothing selected keeps
// it closed, a file selection opens it.
func TestPropertiesDialogGating(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-props")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "文档.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	a := newStoreApp(t, dir)

	a.showProperties()
	if a.propsOpen {
		t.Fatal("opened without a selection")
	}
	a.selectOnly(file)
	a.showProperties()
	if !a.propsOpen {
		t.Fatal("did not open for a file")
	}
	a.propsOpen = false

	// A folder selection counts too — Windows shows folder properties,
	// and its facts load in the background. The folder must be an entry
	// of the current listing, so select a child folder.
	sub := filepath.Join(dir, "子目录")
	os.MkdirAll(sub, 0o755)
	a.reload()
	a.selectOnly(sub)
	a.showProperties()
	if !a.propsOpen {
		t.Fatal("did not open for a folder")
	}
	for i := 0; i < 100; i++ {
		if f := propsState.Load(); f != nil && f.path == sub {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("folder facts never landed")
}
