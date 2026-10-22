package main

import (
	"path/filepath"
	"testing"
)

// TestSearchPlaceRankOrdersCurrentFolderFirst: the pane's search lists the
// folder on screen before a better alphabetical hit further down.
func TestSearchPlaceRankOrdersCurrentFolderFirst(t *testing.T) {
	folder := filepath.Join(string(filepath.Separator), "work", "photos")
	sub := filepath.Join(folder, "sub")
	out := []Entry{
		{Name: "a.txt", Path: filepath.Join(sub, "a.txt"), Dir: sub},
		{Name: "z.txt", Path: filepath.Join(folder, "z.txt"), Dir: folder},
	}
	a := &App{sortKey: SortName, sortAsc: true}
	a.sortSearchEntries(out, folder)
	if out[0].Name != "z.txt" || out[1].Name != "a.txt" {
		t.Fatalf("order = %s, %s, want z.txt then a.txt", out[0].Name, out[1].Name)
	}
}
