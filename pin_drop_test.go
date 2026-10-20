package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// pin_drop_test.go covers a drop on 快速访问: the paths are pinned and left
// where they were, including a file URL, and the sidebar shows them.

// TestPinDropPinsWithoutMoving: a drop of a file and a folder pins both, in
// that order, and does not move either. A repeat is a no-op, and a link or a
// page is refused.
func TestPinDropPinsWithoutMoving(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "pin note.txt")
	folder := filepath.Join(dir, "pin-folder")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, dir)
	a.store.Pinned = nil
	raw := (&url.URL{Scheme: "file", Path: note}).String()
	a.doPinDrop([]string{raw, folder})
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the pinned file was moved or removed: %v", err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("the pinned folder was moved or removed: %v", err)
	}
	got := a.store.pinnedList()
	if len(got) != 2 || got[0] != note || got[1] != folder {
		t.Fatalf("pins = %v, want [%s %s]", got, note, folder)
	}
	if a.statusMsg != "已固定 2 个项目到快速访问" {
		t.Fatalf("status = %q", a.statusMsg)
	}
	places := a.pinnedPlaces()
	if len(places) != 2 || places[0].Kind != "file" || places[1].Kind != "folder" {
		t.Fatalf("places = %+v", places)
	}
	rows := a.navRows()
	qi := -1
	for i, r := range rows {
		if r.pinDrop {
			qi = i
			break
		}
	}
	if qi < 0 || qi+2 >= len(rows) || rows[qi].label != "快速访问" ||
		rows[qi+1].path != note || !rows[qi+1].file || rows[qi+2].path != folder || rows[qi+2].file {
		t.Fatalf("rows around 快速访问 = %+v", rows[qi:])
	}
	if navCanDrag(navRow{path: pathHome}) || !navCanDrag(navRow{path: folder}) || !navCanDrag(navRow{path: note, file: true}) {
		t.Fatal("navCanDrag")
	}

	a.doPinDrop([]string{note})
	if n := len(a.store.pinnedList()); n != 2 || a.statusMsg != "已在快速访问中" {
		t.Fatalf("repeat pin: n=%d status=%q", n, a.statusMsg)
	}
	a.doPinDrop([]string{"https://example.com/a.txt", pathHome, filepath.Join(dir, "missing.txt")})
	if n := len(a.store.pinnedList()); n != 2 || a.statusMsg != "无法固定到快速访问" {
		t.Fatalf("rejected pin: n=%d status=%q", n, a.statusMsg)
	}
}

// TestQuickAccessPinShowsInSidebar drives the real pane: a pinned file and
// folder show under 快速访问, and a click opens the folder or reveals the file.
func TestQuickAccessPinShowsInSidebar(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	other := t.TempDir()
	note := filepath.Join(other, "pin note.txt")
	folder := filepath.Join(other, "pin-folder")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a.store.Pinned = nil
	a.doPinDrop([]string{note, folder})
	tt.Frame()

	if _, ok := tt.Find("快速访问"); !ok {
		t.Fatal("快速访问 heading is not on screen")
	}
	fr, ok := tt.Find("pin-folder")
	if !ok || fr.X > 300 {
		t.Fatalf("sidebar folder pin at %+v ok=%v", fr, ok)
	}
	tt.ClickAt(fr.X+fr.W/2, fr.Y+fr.H/2)
	if a.cur().path != folder {
		t.Fatalf("clicking the pinned folder went to %q", a.cur().path)
	}

	a.navigate(dir)
	tt.Frame()
	nr, ok := tt.Find("pin note.txt")
	if !ok || nr.X > 300 {
		t.Fatalf("sidebar file pin at %+v ok=%v", nr, ok)
	}
	tt.ClickAt(nr.X+nr.W/2, nr.Y+nr.H/2)
	if a.cur().path != other || !a.isSelected(note) {
		t.Fatalf("clicking the pinned file: path=%q selected=%v", a.cur().path, a.isSelected(note))
	}
}
