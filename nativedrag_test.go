package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// nativedrag_test.go covers what the drag paths decide without the native
// toolkit in the loop: what a row offers to other applications, how a drop
// from another application is taken, and where a target's files land.

// TestDragSourceOffersFileURLs: a drag that leaves the window carries the
// paths as a native file list, which is what Finder and Explorer read.
func TestDragSourceOffersFileURLs(t *testing.T) {
	root := t.TempDir()
	a := newStoreApp(t, root)
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if data := dragSourceData(nil); len(data.Formats()) != 0 {
		t.Fatalf("an empty drag offered %v", data.Formats())
	}
	data := dragSourceData(a.dragValueFor(src).paths)
	if _, ok := data.Preferred(transfer.FileList); !ok {
		t.Fatalf("formats = %v, want the system's file list", data.Formats())
	}
	files, err := data.Files()
	if err != nil || len(files) != 1 || files[0] != src {
		t.Fatalf("the dragged files = %q, %v; want %q", files, err, src)
	}
	// File URLs work for paths with spaces and non-ASCII names.
	odd := filepath.Join(root, "名 字 #1.txt")
	if err := os.WriteFile(odd, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = dragSourceData([]string{odd}).Files()
	if err != nil || len(files) != 1 || files[0] != odd {
		t.Fatalf("odd name dragged as %q, %v; want %q", files, err, odd)
	}
}

// TestDropFromAnotherAppCopies: a file list dropped by another application
// lands in the child folder as a copy, and the original stays where it was,
// whatever modifier is held.
func TestDropFromAnotherAppCopies(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "in")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "dropped.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, dir)

	a.doDropInto(dropModifiers(ui.Shift), sub, []string{src})
	if _, err := os.Stat(filepath.Join(sub, "dropped.txt")); err != nil {
		t.Fatalf("the drop did not land: %v (status=%q)", err, a.statusMsg)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the drop moved the original: %v", err)
	}
	if a.statusMsg != "已复制 1 个项目到 sub" {
		t.Fatalf("status = %q", a.statusMsg)
	}
}

// TestDropTargetsThatAreNoFolder: 主文件夹 / 此电脑 are pages, not folders;
// a drag on them must not create a folder of that name.
func TestDropTargetsThatAreNoFolder(t *testing.T) {
	root := t.TempDir()
	a := newStoreApp(t, root)
	src := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(src, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{pathHome, pathThisPC, pathTrash, pathDisks, pathGallery} {
		a.doDropInto(0, page, []string{src})
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
			t.Fatalf("dropping on %q wrote into the shown folder: %v %v", page, entries, err)
		}
	}
	if a.dropDir(filepath.Join(root, "sub")) == "" {
		t.Fatal("a real folder was refused as a target")
	}
}

// TestDropRefusesRemoteURIs keeps a dropped link from becoming a file
// operation.
func TestDropRefusesRemoteURIs(t *testing.T) {
	root := t.TempDir()
	a := newStoreApp(t, root)
	a.doDropInto(0, root, []string{"https://example.com/a.txt"})
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("a link was copied into the folder: %v %v", entries, err)
	}
	if a.statusMsg == "" {
		t.Fatal("the refused drop said nothing")
	}
	if !localPath(filepath.Join(root, "a.txt")) {
		t.Fatal("an absolute path was not local")
	}
}
