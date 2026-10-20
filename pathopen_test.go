package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCommitPathEditSelectsAFile: typing a file into the address bar opens
// the folder it lives in, with that file selected and asked into view,
// rather than trying to list the file as a folder.
func TestCommitPathEditSelectsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, dir)
	a.navigate(t.TempDir())
	a.commitPathEdit(file)
	if got := a.cur().path; got != dir {
		t.Fatalf("path = %q, want the file's folder %q", got, dir)
	}
	if !a.cur().selected[file] {
		t.Fatalf("selection = %v, want %q", a.cur().selected, file)
	}
	if a.cur().reveal != file {
		t.Fatalf("reveal = %q, want the file raised into view", a.cur().reveal)
	}
}

// TestCommitPathEditEntersAFolder: a folder in the address bar is entered,
// and nothing in it is selected — opening a folder is not the same as
// coming back up out of one.
func TestCommitPathEditEntersAFolder(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "box")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, dir)
	a.commitPathEdit(sub)
	if got := a.cur().path; got != sub {
		t.Fatalf("path = %q, want %q", got, sub)
	}
	if len(a.cur().selected) != 0 {
		t.Fatalf("entering a folder selected %v", a.cur().selected)
	}
}
