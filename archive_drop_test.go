package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func newTestApp(t *testing.T, dir string) *App {
	t.Helper()
	// Every UI test gets its own lights: the AI fx store is shared with
	// tool tests that leave rows glowing.
	isolateAIFx(t)
	a := &App{
		tabs:        []*tabState{newTab(dir)},
		activeTab:   0,
		sortKey:     SortName,
		sortAsc:     true,
		homePath:    dir,
		previewOpen: false,
		// The bare test app takes the zero value of the sidebar's expanded
		// state, which is collapsed; the pane's sections start open, as the
		// real window's do.
		navQuickExpanded:  true,
		navRecentExpanded: true,
	}
	a.reload()
	return a
}

func TestArchiveRoundTrip(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-arch")
	defer os.RemoveAll(root)
	src := filepath.Join(root, "photos")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644)
	os.WriteFile(filepath.Join(root, "c.txt"), []byte("loose"), 0o644)

	a := newTestApp(t, root)
	a.selectAll()
	if len(a.selectedEntries()) != 2 {
		t.Fatalf("selection = %d", len(a.selectedEntries()))
	}
	a.doCompress()
	// 多选压缩命名为“压缩文件”，与状态提示一致。
	zipPath := filepath.Join(root, "压缩文件.zip")
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip missing: %v (status=%q)", err, a.statusMsg)
	}
	// Round-trip: extract and compare contents.
	if err := extractArchive(zipPath, filepath.Join(root, "out")); err != nil {
		t.Fatalf("extract zip: %v", err)
	}
	want := map[string]string{"photos/a.txt": "hello", "photos/sub/b.txt": "world"}
	for rel, content := range want {
		b, err := os.ReadFile(filepath.Join(root, "out", rel))
		if err != nil || string(b) != content {
			t.Fatalf("member %s = %q, %v", rel, b, err)
		}
	}

	// tar.gz via system tar.
	tgz := filepath.Join(root, "tree.tar.gz")
	cmd := exec.Command("tar", "-czf", tgz, "-C", root, "c.txt")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("mk tar: %v %s", e, b)
	}
	if err := extractArchive(tgz, filepath.Join(root, "tout")); err != nil {
		t.Fatalf("extract tgz: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "tout", "c.txt")); err != nil || string(b) != "loose" {
		t.Fatalf("tgz content: %q %v", b, err)
	}

	// Malicious zip with a traversal member must fail closed.
	evil := filepath.Join(root, "evil.zip")
	zw := zip.NewWriter(mustCreate(t, evil))
	w, _ := zw.Create("../evil.txt")
	w.Write([]byte("bad"))
	zw.Close()
	err := extractArchive(evil, filepath.Join(root, "eout"))
	if err == nil || !strings.Contains(err.Error(), "不安全") {
		t.Fatalf("zip-slip not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); err == nil {
		t.Fatalf("zip-slip escaped the target dir")
	}
}

func TestUniqueFreeName(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-name")
	defer os.RemoveAll(dir)
	if n, err := uniqueFreeName(dir, "a.txt", false); err != nil || n != "a.txt" {
		t.Fatalf("free name: %q %v", n, err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	if n, err := uniqueFreeName(dir, "a.txt", false); err != nil || n != "a - 副本.txt" {
		t.Fatalf("first conflict: %q %v", n, err)
	}
	os.WriteFile(filepath.Join(dir, "a - 副本.txt"), nil, 0o644)
	if n, err := uniqueFreeName(dir, "a.txt", false); err != nil || n != "a - 副本 (2).txt" {
		t.Fatalf("second conflict: %q %v", n, err)
	}
}

func TestDropMoveCopy(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-drop")
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "in")
	dst := filepath.Join(root, "dst")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(dst, 0o755)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	a := newTestApp(t, dir)

	// Move into dst.
	a.doDropInto(0, dst, []string{filepath.Join(dir, "f.txt")})
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Fatalf("move failed: %v (status=%q)", err, a.statusMsg)
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !os.IsNotExist(err) {
		t.Fatalf("source not removed")
	}

	// Copy back with a modifier held.
	a.doDropInto(ui.Cmd, dst, []string{filepath.Join(dst, "f.txt")})
	if _, err := os.Stat(filepath.Join(dst, "f - 副本.txt")); err != nil {
		t.Fatalf("copy failed: %v (status=%q)", err, a.statusMsg)
	}

	// Same-folder move is a no-op.
	a.doDropInto(0, dst, []string{filepath.Join(dst, "f.txt")})
	if _, err := os.Stat(filepath.Join(dst, "f - 副本 (2).txt")); err == nil {
		t.Fatalf("same-dir move should be a no-op")
	}

	// Dropping a folder into itself is skipped.
	a.doDropInto(0, dst, []string{dst})
	if _, err := os.Stat(filepath.Join(dst, "dst")); !os.IsNotExist(err) {
		t.Fatalf("folder moved into itself")
	}
}

func mustCreate(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
