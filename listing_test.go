package main

import (
	"os"
	"path/filepath"
	"testing"
)

// listingDir makes a folder with a file, a subfolder and a symlink to the
// subfolder, the three cases the lister's stat pass tells apart.
func listingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLocalLister reads the folder the pane lists and checks both halves of
// the contract at once: the parallel pass (the network mount's reader)
// produces exactly the serial pass's entries — same order, same fields — and
// those entries say what the pane needs — the file's size and extension, the
// folders including the one a symlink points at, and a modification time.
func TestLocalLister(t *testing.T) {
	dir := listingDir(t)
	serial, err := (localLister{}).List(dir)
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := (localLister{parallel: true}).List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(serial) != len(parallel) {
		t.Fatalf("serial listed %d entries, parallel %d", len(serial), len(parallel))
	}
	for i := range serial {
		s, p := serial[i], parallel[i]
		if s.Name != p.Name || s.Path != p.Path || s.IsDir != p.IsDir ||
			s.Size != p.Size || s.Ext != p.Ext || !s.ModTime.Equal(p.ModTime) {
			t.Fatalf("entry %d: serial %+v, parallel %+v", i, s, p)
		}
	}

	byName := map[string]Entry{}
	for _, e := range parallel {
		byName[e.Name] = e
	}
	if e := byName["sub"]; !e.IsDir {
		t.Fatalf("sub is not a folder: %+v", e)
	}
	if e := byName["a.txt"]; e.IsDir || e.Size != 5 || e.Ext != "txt" {
		t.Fatalf("a.txt = %+v, want a 5-byte txt file", e)
	}
	if e := byName["link"]; !e.IsDir {
		t.Fatalf("link to a folder is not a folder: %+v", e)
	}
	if byName["a.txt"].ModTime.IsZero() {
		t.Fatal("a.txt carries no modification time")
	}
}

// TestLocalListerEdges: an empty folder lists nothing and is no error; a
// missing folder reports the read's error, which reload shows as the pane's
// message.
func TestLocalListerEdges(t *testing.T) {
	entries, err := (localLister{parallel: true}).List(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty folder listed %d entries", len(entries))
	}
	if _, err := (localLister{}).List(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("listing a missing folder returned no error")
	}
}

// TestNetworkPathPicksTheReader: a local folder — which is what the tests'
// temp folders are — is not a network path, the answer is the same on the
// cached second ask, and the folder therefore gets the serial reader. The
// choice is the mount's, not the path's shape.
func TestNetworkPathPicksTheReader(t *testing.T) {
	dir := t.TempDir()
	forgetNetworkPath(dir)
	if isNetworkPath(dir) {
		t.Fatalf("%s reads as a network path", dir)
	}
	if isNetworkPath(dir) {
		t.Fatalf("%s reads as a network path on the cached answer too", dir)
	}
	if isNetworkPath("") {
		t.Fatal("the empty path reads as a network path")
	}
	l, ok := entryLister(dir).(localLister)
	if !ok {
		t.Fatalf("entryLister returned %T, want localLister", entryLister(dir))
	}
	if l.parallel {
		t.Fatal("a local temp folder asked for the parallel reader")
	}
}

// TestReloadUsesTheLister: the pane's listing comes from the lister, so
// reload sees the folder's files with their sizes.
func TestReloadUsesTheLister(t *testing.T) {
	dir := listingDir(t)
	a := newStoreApp(t, dir)
	a.navigate(dir)
	var names []string
	for _, e := range a.cur().entries {
		names = append(names, e.Name)
	}
	for _, want := range []string{"a.txt", "sub", "link"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("reload listed %v, want %s among them", names, want)
		}
	}
}

// TestSettingsDefaultOn: a state file from before the two switches existed
// — neither field stored — reads as warm-up on and session restore on, so an
// upgrade keeps the first tiles decoding and the tabs coming back without
// anyone touching 设置.
func TestSettingsDefaultOn(t *testing.T) {
	var s appSettings
	if !s.networkWarmup() {
		t.Fatal("network thumbnail warm-up is off by default")
	}
	if !s.restoreSession() {
		t.Fatal("session restore is off by default")
	}
	s.NoNetworkWarmup = true
	if s.networkWarmup() {
		t.Fatal("the stored switch did not turn warm-up off")
	}
	s.NoRestoreSession = true
	if s.restoreSession() {
		t.Fatal("the stored switch did not turn session restore off")
	}
}
