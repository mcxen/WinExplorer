package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIFlagsVersionExitsWithoutGUI(t *testing.T) {
	var b bytes.Buffer
	if !cliFlags([]string{"--version"}, &b) || !strings.Contains(b.String(), "Sunvale") {
		t.Fatalf("--version: %q", b.String())
	}
	if cliFlags([]string{"/tmp"}, &b) {
		t.Fatal("a path must start the GUI")
	}
}

func TestCLITargetsDirFileRelativeHome(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	file := filepath.Join(sub, "a.txt")
	os.WriteFile(file, nil, 0o644)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	got := cliTargets([]string{"--flag", "sub", "sub/a.txt", "~", "missing"}, dir)
	want := []pathTarget{{dir: sub}, {dir: sub, file: file}, {dir: dir}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
