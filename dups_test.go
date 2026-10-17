package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests here are logic-level: the duplicate grouping, the subtree
// scan, the digest cache and the delete-extras trash path. No GUI is
// started; background results are polled for with bounded waits.

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryAt(t *testing.T, path string) Entry {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return Entry{Name: filepath.Base(path), Path: path, Size: info.Size(), ModTime: info.ModTime()}
}

func entryNames(group []Entry) []string {
	out := make([]string, len(group))
	for i, e := range group {
		out[i] = e.Name
	}
	return out
}

// waitUntil polls ok until it holds, as the background loads land off the
// main thread.
func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 250; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the background work")
}

func TestGroupDuplicates(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-dups")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	writeFile(t, filepath.Join(root, "a.txt"), "dup")
	writeFile(t, filepath.Join(root, "b.txt"), "dup")
	writeFile(t, filepath.Join(root, "c.txt"), "other")
	writeFile(t, filepath.Join(root, "sub", "d.txt"), "dup")
	writeFile(t, filepath.Join(root, "p.bin"), "same")
	writeFile(t, filepath.Join(root, "q.bin"), "same")

	files := []Entry{
		entryAt(t, filepath.Join(root, "a.txt")),
		entryAt(t, filepath.Join(root, "b.txt")),
		entryAt(t, filepath.Join(root, "c.txt")),
		entryAt(t, filepath.Join(root, "sub", "d.txt")),
		entryAt(t, filepath.Join(root, "p.bin")),
		entryAt(t, filepath.Join(root, "q.bin")),
		// A file that cannot be read is not provably a duplicate.
		{Name: "ghost", Path: filepath.Join(root, "ghost"), Size: 3},
	}
	groups := groupDuplicates(files, fileSHA256)
	if len(groups) != 2 {
		t.Fatalf("groups = %d: %v", len(groups), groups)
	}
	// Groups in first-seen size order: size 3 before size 4.
	first, second := groups[0], groups[1]
	if len(first) != 3 || first[0].Name != "a.txt" || first[1].Name != "b.txt" || first[2].Name != "d.txt" {
		t.Fatalf("first group = %v", entryNames(first))
	}
	if len(second) != 2 || second[0].Name != "p.bin" || second[1].Name != "q.bin" {
		t.Fatalf("second group = %v", entryNames(second))
	}
	for _, g := range groups {
		for _, e := range g {
			if e.Name == "c.txt" || e.Name == "ghost" {
				t.Fatalf("%s grouped: %v", e.Name, entryNames(g))
			}
		}
	}
}

func TestDupsScan(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-dupscan")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	writeFile(t, filepath.Join(root, "a.txt"), "dup-content")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "dup-content")
	writeFile(t, filepath.Join(root, "c.txt"), "unique")
	// Hidden entries are skipped unless the user shows them.
	writeFile(t, filepath.Join(root, ".hidden"), "dup-content")

	a := newStoreApp(t, root)
	a.requestDups()
	waitUntil(t, func() bool {
		res := dupsResult.Load()
		return res != nil && res.root == root
	})
	res := dupsResult.Load()
	if res.files != 3 {
		t.Fatalf("scanned files = %d, want 3", res.files)
	}
	if len(res.groups) != 1 || len(res.groups[0]) != 2 {
		t.Fatalf("groups = %v", res.groups)
	}
	g := res.groups[0]
	if g[0].Name != "a.txt" || g[1].Name != "b.txt" {
		t.Fatalf("group = %v", entryNames(g))
	}
	if !strings.HasSuffix(filepath.Dir(g[1].Path), "sub") {
		t.Fatalf("subtree not scanned: %q", g[1].Path)
	}
}

func TestHashCache(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-hash")
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "文档.txt")
	writeFile(t, f, "hello")
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])
	a := newStoreApp(t, dir)

	a.requestHash(f)
	waitUntil(t, func() bool {
		v, ready := hashRowValue(f)
		return ready && v == want
	})
	if hashComputes.Load() != 1 {
		t.Fatalf("computes = %d, want 1", hashComputes.Load())
	}

	// The same unchanged file is served from the cache, not recomputed.
	a.requestHash(f)
	a.requestHash(f)
	if hashComputes.Load() != 1 {
		t.Fatalf("recomputed for an unchanged file: %d", hashComputes.Load())
	}
	if v, ready := hashRowValue(f); !ready || v != want {
		t.Fatalf("cached value = %q %v", v, ready)
	}

	// A file that changed on disk hashes again.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(f, future, future); err != nil {
		t.Fatal(err)
	}
	a.requestHash(f)
	waitUntil(t, func() bool {
		_, ready := hashRowValue(f)
		return ready
	})
	if hashComputes.Load() != 2 {
		t.Fatalf("computes = %d, want 2 after the modtime changed", hashComputes.Load())
	}
}

func TestDupsDeleteExtras(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-dupdel")
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "工作")
	os.MkdirAll(dir, 0o755)
	keep := filepath.Join(dir, "a.txt")
	lose := filepath.Join(dir, "b.txt")
	writeFile(t, keep, "dup-content")
	writeFile(t, lose, "dup-content")
	past := time.Now().AddDate(0, 0, -1)
	os.Chtimes(keep, past, past) // the keeper is the older one

	a := newStoreApp(t, dir)
	group := []Entry{entryAt(t, keep), entryAt(t, lose)}
	if dupsOldest(group) != 0 {
		t.Fatal("oldest pick")
	}
	if victims := dupsVictims(group, 0); len(victims) != 1 || victims[0].Name != "b.txt" {
		t.Fatalf("victims = %v", entryNames(victims))
	}
	moved, failed := a.dupsTrash(dupsVictims(group, 0))
	if moved != 1 || len(failed) != 0 {
		t.Fatalf("dupsTrash = %d moved, %v failed (status %q)", moved, failed, a.statusMsg)
	}
	if _, err := os.Stat(lose); !os.IsNotExist(err) {
		t.Fatal("extra not moved to trash")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("keeper deleted")
	}
	if !a.canUndo() {
		t.Fatal("delete not recorded for undo")
	}
	// The trash log knows the extra by the name it landed under; the real
	// trash can hold other items, so find ours through the log.
	logged := false
	for _, e := range trashEntries() {
		if orig, ok := a.store.originalForTrash(e.Name); ok && orig == lose {
			logged = true
			break
		}
	}
	if !logged {
		t.Fatal("trash log does not know the extra")
	}
	a.doUndo()
	if _, err := os.Stat(lose); err != nil {
		t.Fatalf("undo did not restore: %v", err)
	}
}
