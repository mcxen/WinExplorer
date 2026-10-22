package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// archive_async_test.go drives the background unpack: the source rows
// breathe while the work runs, the fresh folders flash when they land, and
// a full success lines the originals up for the toast's 删除原归档.

func TestDoExtractAsyncBreathesAndFinishes(t *testing.T) {
	fx, clk := swapTestFx(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "pack.zip")
	writeNestedZip(t, src, [][2]string{{"hello.txt", "hi"}})
	a := newStoreApp(t, dir)
	a.reload()
	a.selectOnly(src)

	a.doExtract()
	// The row is lit before the goroutine starts: the picker's answer.
	if n := fx.activeNow(); n != 1 {
		t.Fatalf("busy during extract = %d", n)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(a.statusMsg, "已解压") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(a.statusMsg, "已解压 1 个归档") {
		t.Fatalf("status %q", a.statusMsg)
	}
	// Done: the source's breath is out, the new folder flashes on its row,
	// and the originals are queued for the toast's delete offer.
	if n := fx.activeNow(); n != 0 {
		t.Fatalf("busy after extract = %d", n)
	}
	dst := filepath.Join(dir, "pack")
	if l := fx.look(dst); l.flash <= 0 {
		t.Fatalf("the new folder did not flash: %+v", l)
	}
	if len(a.extractSources) != 1 || a.extractSources[0] != src {
		t.Fatalf("extractSources = %v", a.extractSources)
	}
	if body, err := os.ReadFile(filepath.Join(dst, "hello.txt")); err != nil || string(body) != "hi" {
		t.Fatalf("content: %q %v", body, err)
	}
	// A failed archive lights up and goes out all the same.
	bad := filepath.Join(dir, "bad.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reload()
	a.selectOnly(bad)
	a.doExtract()
	clk.add(time.Millisecond)
	deadline = time.Now().Add(3 * time.Second)
	for !strings.Contains(a.statusMsg, "解压失败") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(a.statusMsg, "解压失败") {
		t.Fatalf("bad status %q", a.statusMsg)
	}
	if n := fx.activeNow(); n != 0 {
		t.Fatalf("busy after failure = %d", n)
	}
	if len(a.extractSources) != 1 || a.extractSources[0] != src {
		t.Fatalf("a failed run must not offer deletion: %v", a.extractSources)
	}
}

// TestTrashPathsUndoable: the toast's 删除原归档 lands the originals in
// the bin with an undo entry, as a pane delete does.
func TestTrashPathsUndoable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "keep.zip")
	writeNestedZip(t, src, [][2]string{{"a.txt", "a"}})
	a := newStoreApp(t, dir)
	seq := a.undoSeq
	a.trashPaths([]string{src})
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("the archive stayed: %v", err)
	}
	if a.undoSeq == seq {
		t.Fatal("no undo entry for the delete")
	}
	if a.statusMsg == "" {
		t.Fatal("no outcome line")
	}
}
