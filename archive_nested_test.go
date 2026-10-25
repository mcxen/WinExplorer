package main

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archive_nested_test.go drives the recursive extractor: real nested zips
// through the real engine, password scenarios through the engine variable,
// and the unzip tool's end-to-end message.

// writeNestedZip makes path a zip with members in name order.
func writeNestedZip(t *testing.T, path string, members [][2]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, m := range members {
		w, err := zw.Create(m[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// nestedZipFixture is l3.txt inside l3.zip inside l2.zip inside l1.zip,
// with a plain.txt beside the l2 level.
func nestedZipFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	l3 := filepath.Join(dir, "l3.zip")
	writeNestedZip(t, l3, [][2]string{{"l3.txt", "最深层的信"}})
	l3b, _ := os.ReadFile(l3)
	l2 := filepath.Join(dir, "l2.zip")
	writeNestedZip(t, l2, [][2]string{{"l3.zip", string(l3b)}, {"plain.txt", "同层的伴儿"}})
	l2b, _ := os.ReadFile(l2)
	l1 := filepath.Join(dir, "l1.zip")
	writeNestedZip(t, l1, [][2]string{{"l2.zip", string(l2b)}})
	return l1
}

// TestExtractArchiveNestedThreeLevels: one call opens the whole chain and
// reports the inner archives it opened.
func TestExtractArchiveNestedThreeLevels(t *testing.T) {
	src := nestedZipFixture(t)
	dst := filepath.Join(t.TempDir(), "out")
	_, r, err := extractArchiveNested(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Extracted) != 2 || r.Locked != nil || r.Failed != nil {
		t.Fatalf("report = %+v", r)
	}
	body, err := os.ReadFile(filepath.Join(dst, "l2", "l3", "l3.txt"))
	if err != nil || string(body) != "最深层的信" {
		t.Fatalf("deepest file: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "l2", "plain.txt")); err != nil {
		t.Fatal("the top level's sibling file is missing")
	}
	if got := r.summary(); !strings.Contains(got, "自动解开 2 个内层归档") {
		t.Fatalf("summary %q", got)
	}
}

// TestExtractNestedLockedInner: an inner archive the book cannot open
// stays named in the report, while the rest of the tree still lands.
func TestExtractNestedLockedInner(t *testing.T) {
	src := nestedZipFixture(t)
	old := extractWithPasswordsFn
	extractWithPasswordsFn = func(archive, dst string, passwords []string, ask passwordPrompt) (string, error) {
		if strings.HasSuffix(archive, "l3.zip") {
			return "", fmt.Errorf("%w；4 个密码都不对", errArchivePassword)
		}
		return old(archive, dst, passwords, ask)
	}
	t.Cleanup(func() { extractWithPasswordsFn = old })

	dst := filepath.Join(t.TempDir(), "out")
	_, r, err := extractArchiveNested(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Locked) != 1 || !strings.Contains(r.Locked[0], "l3.zip") {
		t.Fatalf("locked = %v", r.Locked)
	}
	if len(r.Extracted) != 1 {
		t.Fatalf("extracted = %v", r.Extracted)
	}
	if _, err := os.Stat(filepath.Join(dst, "l2", "l3.zip")); err != nil {
		t.Fatal("the locked inner archive should still be there untouched")
	}
	if got := r.summary(); !strings.Contains(got, "1 个内层归档密码本没解开") {
		t.Fatalf("summary %q", got)
	}
}

// TestExtractNestedReprioritizes: the password that won the parent archive
// is tried first on the inner ones — a password-folder family costs one
// miss, not one per member.
func TestExtractNestedReprioritizes(t *testing.T) {
	src := nestedZipFixture(t)
	old := extractWithPasswordsFn
	var tries [][]string
	extractWithPasswordsFn = func(archive, dst string, passwords []string, ask passwordPrompt) (string, error) {
		tries = append(tries, append([]string(nil), passwords...))
		pwd, err := old(archive, dst, passwords, ask)
		if err == nil {
			pwd = "family-secret"
		}
		return pwd, err
	}
	t.Cleanup(func() { extractWithPasswordsFn = old })

	dst := filepath.Join(t.TempDir(), "out")
	book := []string{"first", "family-secret", "last"}
	if _, _, err := extractArchiveNested(src, dst, book); err != nil {
		t.Fatal(err)
	}
	if len(tries) < 2 {
		t.Fatalf("tries = %v", tries)
	}
	for i, cand := range tries[1] {
		if cand != tries[1][i] {
			break
		}
	}
	if tries[1][0] != "family-secret" {
		t.Fatalf("inner archives did not get the winner first: %v", tries[1])
	}
}

// TestExtractNestedSelfContainedStops: an archive whose payload is another
// archive in an endless family stops at the walker’s bounds instead of
// unpacking forever.
func TestExtractNestedSelfContainedStops(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "self.zip")
	// A chain of loch-ness: m.zip contains m.zip… the walk must count.
	inner := "payload"
	for i := 0; i < nestedMaxDepth+3; i++ {
		tmp := filepath.Join(dir, "m.zip")
		writeNestedZip(t, tmp, [][2]string{{"m.zip", inner}})
		b, _ := os.ReadFile(tmp)
		inner = string(b)
	}
	writeNestedZip(t, src, [][2]string{{"m.zip", inner}})
	dst := filepath.Join(dir, "out")
	_, r, err := extractArchiveNested(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Extracted) == 0 || len(r.Extracted) > nestedMaxCount {
		t.Fatalf("extracted = %d, unbounded", len(r.Extracted))
	}
}

// TestUnzipToolNestedOutput: the assistant's unzip reports the inner
// archives it opened in one call.
func TestUnzipToolNestedOutput(t *testing.T) {
	src := nestedZipFixture(t)
	dir := filepath.Dir(src)
	sunvaleToolState.bind(dir, false, nil, nil, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "unzip", map[string]any{"archive": filepath.Base(src)})
	if !strings.Contains(got, "已解压到") || !strings.Contains(got, "自动解开 2 个内层归档") {
		t.Fatalf("unzip: %s", got)
	}
	// The confirm-mode next step comes with the result.
	if !strings.Contains(got, "下一步") || !strings.Contains(got, "trash_items") {
		t.Fatalf("no next step offered: %s", got)
	}
	out := filepath.Join(dir, "l1")
	if body, err := os.ReadFile(filepath.Join(out, "l2", "l3", "l3.txt")); err != nil || string(body) != "最深层的信" {
		t.Fatalf("nested result: %q %v", body, err)
	}
	// Autopilot keeps its own counsel: no prompt-shaped next step.
	sunvaleToolState.bind(dir, false, nil, nil, true)
	src2 := nestedZipFixture(t)
	autosrc := filepath.Join(dir, "auto.zip")
	if err := os.Rename(src2, autosrc); err != nil {
		t.Fatal(err)
	}
	got = callToolText(t, sess, "unzip", map[string]any{"archive": "auto.zip", "dest": "auto"})
	if strings.Contains(got, "下一步") {
		t.Fatalf("autopilot got a choice prompt: %s", got)
	}
	sunvaleToolState.bind(dir, false, nil, nil, false)
}
