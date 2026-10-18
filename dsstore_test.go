package main

import (
	"os"
	"path/filepath"
	"testing"
)

// dsstore_test.go drives the 清理 .DS_Store plugin: the scan that finds Finder's
// metadata files under a folder, and the delete that moves them into the 回收站.

// TestScanDSStore: the walk finds the .DS_Store files under root — in hidden
// folders too, which is where they live — and nothing else.
func TestScanDSStore(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-ds")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	writeFile(t, filepath.Join(root, "keep.txt"), "data")
	writeFile(t, filepath.Join(root, ".DS_Store"), "aaa")
	writeFile(t, filepath.Join(root, "sub", ".DS_Store"), "bb")
	writeFile(t, filepath.Join(root, ".git", ".DS_Store"), "c")
	writeFile(t, filepath.Join(root, ".DS_Store.backup"), "not the name")

	res := scanDSStore(root)
	if res.truncated {
		t.Fatal("a four-file tree hit the scan's caps")
	}
	if len(res.files) != 3 {
		t.Fatalf("found %v, want the three .DS_Store files", entryNames(res.files))
	}
	if res.bytes != 6 {
		t.Fatalf("bytes = %d, want 6", res.bytes)
	}
}

// TestDSStoreMenuItems: the item shows for the pane background's menu and for
// one folder's row, and not for a file, a multi-selection or a special page.
func TestDSStoreMenuItems(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-ds")
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "工作")
	os.MkdirAll(dir, 0o755)
	a := newStoreApp(t, root)
	hasItem := func(sel []Entry) bool {
		for _, it := range a.pluginMenuItems(sel) {
			if it.Label == "清理 .DS_Store" {
				return true
			}
		}
		return false
	}
	if !hasItem(nil) {
		t.Fatal("no item on the pane background's menu")
	}
	if !hasItem([]Entry{{Name: dir, Path: dir, IsDir: true}}) {
		t.Fatal("no item on a folder row's menu")
	}
	if hasItem([]Entry{{Name: "f.txt", Path: filepath.Join(root, "f.txt")}}) {
		t.Fatal("item on a file row's menu")
	}
	if hasItem([]Entry{
		{Name: "a", Path: filepath.Join(root, "a"), IsDir: true},
		{Name: "b", Path: filepath.Join(root, "b"), IsDir: true},
	}) {
		t.Fatal("item on a multi-selection's menu")
	}
	a.navigate(pathThisPC)
	if hasItem(nil) {
		t.Fatal("item on the 此电脑 page")
	}
}

// TestDSStoreTrash: 全部删除's mover puts every .DS_Store into the trash, keeps
// everything else, logs each for the 回收站 and stacks one undo that puts them
// back.
func TestDSStoreTrash(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-ds")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	keep := filepath.Join(root, "keep.txt")
	one := filepath.Join(root, ".DS_Store")
	two := filepath.Join(root, "sub", ".DS_Store")
	writeFile(t, keep, "data")
	writeFile(t, one, "aaaa")
	writeFile(t, two, "bb")

	a := newStoreApp(t, root)
	res := scanDSStore(root)
	if len(res.files) != 2 {
		t.Fatalf("scan found %v", entryNames(res.files))
	}
	if moved, failed := a.dsstoreTrash(res.files); moved != 2 || len(failed) != 0 {
		t.Fatalf("dsstoreTrash = %d moved, %v failed (status %q)", moved, failed, a.statusMsg)
	}
	for _, gone := range []string{one, two} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%q not moved to the trash", gone)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("the ordinary file went with them")
	}
	if !a.canUndo() {
		t.Fatal("the cleanup is not on the undo stack")
	}
	if len(a.store.TrashLog) != 2 {
		t.Fatalf("trash log = %d records, want 2", len(a.store.TrashLog))
	}
	// Ctrl+Z puts them back where they were.
	a.doUndo()
	for _, back := range []string{one, two} {
		if _, err := os.Stat(back); err != nil {
			t.Fatalf("undo did not restore %q: %v", back, err)
		}
	}
}

// TestDSStoreDialog: the dialog the menu opens lists what the scan found, with
// the 全部删除 button beside 关闭.
func TestDSStoreDialog(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	writeFile(t, filepath.Join(root, ".DS_Store"), "aaa")
	writeFile(t, filepath.Join(root, "sub", ".DS_Store"), "bb")

	a, tt, _ := keynavWindow(t, 0)
	a.navigate(root)
	a.openDSStore(root)
	waitUntil(t, func() bool { return !dsstoreRunner.busy() })
	tt.Frame()
	if !*a.dsstoreOpenFlag() {
		t.Fatal("the dialog did not open")
	}
	for _, text := range []string{"清理 .DS_Store", "2 个 .DS_Store", "全部删除"} {
		if !tt.HasText(text) {
			t.Fatalf("the dialog does not show %q: %v", text, tt.Texts())
		}
	}
	// Closing takes it down without touching anything.
	tt.Click("关闭")
	tt.Frame()
	if *a.dsstoreOpenFlag() {
		t.Fatal("the dialog stayed open")
	}
	if _, err := os.Stat(filepath.Join(root, ".DS_Store")); err != nil {
		t.Fatal("关闭 deleted something")
	}
}
