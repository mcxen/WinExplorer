package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// searchbox_test.go drives the Ctrl+K palette in a laid-out window: the
// shortcut, the streamed hits of its list, the cursor keys and the ways out.

// waitSearchBox runs frames until the box's search finishes, bounded so a wedged
// search fails the test instead of hanging it. The frames are the point: the
// box applies what the search hands over (frameQueue) in a frame, so a test that
// wants the hits must pump them.
func waitSearchBox(t *testing.T, tt *ui.Tester, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if !a.searchBox.busy {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the search box's walk never finished")
}

// TestCtrlKSearchBox raises the palette with Ctrl+K, searches the tab's folder
// — the whole machine is far too big to walk in a test, so it takes the folder
// scope the switch offers — and opens a hit.
func TestCtrlKSearchBox(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	// Nested hits keep the palette's labels unique: the pane shows the
	// folders but not the files below them.
	for _, sub := range []string{"alpha-dir", "beta-dir", "gamma-dir"} {
		subdir := filepath.Join(dir, sub)
		if err := os.Mkdir(subdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "nested-"+sub+".txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !a.searchBox.open {
		t.Fatal("Ctrl+K did not raise the search box")
	}
	// The box is the machine-wide one: it starts at 全部磁盘.
	if a.searchBox.scope != scopeGlobal {
		t.Fatalf("the box opened at scope %v, want 全部磁盘", a.searchBox.scope)
	}
	a.searchBox.scope = scopeFolder
	tt.Type("nested")
	tt.Frame()
	waitSearchBox(t, tt, a)
	if len(a.searchBox.results) != 3 {
		t.Fatalf("the box found %d hits, want the folder's three", len(a.searchBox.results))
	}
	if !tt.HasText("nested-") {
		t.Fatalf("the list does not show its hits; the window shows %v", tt.Texts())
	}
	// The hits are laid out as rows: two of them stack at the same left edge,
	// 44 DIP apart — the list inside the dialog has a real height.
	firstBreadcrumb := searchPathText(searchPathSegments(a.searchBox.results[0].Dir, homeForSearch(), 5))
	secondBreadcrumb := searchPathText(searchPathSegments(a.searchBox.results[1].Dir, homeForSearch(), 5))
	firstRow, okFirst := tt.Find(firstBreadcrumb)
	secondRow, okSecond := tt.Find(secondBreadcrumb)
	if !okFirst || !okSecond {
		t.Fatalf("the hits are not in the window: %v / %v", firstRow, secondRow)
	}
	if firstRow.X != secondRow.X {
		t.Fatalf("the rows are not in one column: %v / %v", firstRow, secondRow)
	}
	if d := secondRow.Y - firstRow.Y; d < 40 || d > 50 {
		t.Fatalf("the rows are %v apart, want a 44 DIP row", d)
	}
	for _, hit := range a.searchBox.results {
		if _, ok := tt.Find(hit.Name); !ok {
			t.Fatalf("the palette does not show the complete name %q: %v", hit.Name, tt.Texts())
		}
	}
	// The row's tooltip is its full path, not the breadcrumb it can shorten.
	row, ok := tt.Find(a.searchBox.results[0].Name)
	if !ok {
		t.Fatal("the first palette row vanished")
	}
	tt.Move(2, 2)
	tt.Advance(time.Second)
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Advance(time.Second)
	if !tt.HasText(a.searchBox.results[0].Path) {
		t.Fatalf("the row does not show its full path as a tooltip: %v", tt.Texts())
	}

	// The cursor walks the hits and holds at the ends. The keys stay with the
	// box: the pane's own selection is untouched.
	tt.Key(0, ui.KeyUp)
	if a.searchBox.cursor != 0 {
		t.Fatalf("↑ from the first hit moved the cursor to %d", a.searchBox.cursor)
	}
	tt.Key(0, ui.KeyDown)
	if a.searchBox.cursor != 1 {
		t.Fatalf("↓ moved the cursor to %d, want 1", a.searchBox.cursor)
	}
	if n := len(a.cur().selected); n != 0 {
		t.Fatalf("the box's ↑/↓ moved the pane's selection to %d items", n)
	}
	hit, ok := a.searchBox.hit()
	if !ok {
		t.Fatal("no hit under the cursor")
	}

	// Ctrl+Enter shows the hit in the pane: the tab selects it, and the box
	// takes itself down.
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("the box stayed open after Ctrl+Enter")
	}
	navIs(t, a, "Ctrl+Enter on a hit", hit.Name)
	if a.cur().path != hit.Dir {
		t.Fatalf("the tab is in %q, want the hit's folder %q", a.cur().path, hit.Dir)
	}

	// Escape closes the box, and the next Ctrl+K starts from an empty one.
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !a.searchBox.open {
		t.Fatal("Ctrl+K did not raise the box a second time")
	}
	tt.Type("f0")
	tt.Frame()
	if a.searchBox.query != "f0" {
		t.Fatalf("query = %q, want what was typed", a.searchBox.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("Escape did not close the box")
	}
	if a.searchBox.query != "" || len(a.searchBox.results) != 0 {
		t.Fatalf("the closed box kept query %q and %d hits", a.searchBox.query, len(a.searchBox.results))
	}
}

// TestSearchDetailsShowRelativeLocation: the pane's 位置 column keeps the
// folders below the search root, not just the folder holding the hit.
func TestSearchDetailsShowRelativeLocation(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	nested := filepath.Join(dir, "reports", "2026", "q4")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "needle.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mode = ViewDetails
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyE)
	tt.Type("needle")
	tt.Frame()
	waitSearch(t, a)
	tt.Frame()
	if !tt.HasText("reports › 2026 › q4") {
		t.Fatalf("the 位置 column does not keep the relative path: %v", tt.Texts())
	}
}

// TestSearchBoxScopeSwitchIsTheWalkKey pins the switch: the same query over
// another scope starts a new walk, and the two scopes are the ones searchRoots
// knows.
func TestSearchBoxScopeSwitchIsTheWalkKey(t *testing.T) {
	if scopeIndex(scopeFolder) == scopeIndex(scopeGlobal) {
		t.Fatal("both scopes share a switch slot")
	}
	if scopeFromIndex(scopeIndex(scopeFolder)) != scopeFolder ||
		scopeFromIndex(scopeIndex(scopeGlobal)) != scopeGlobal {
		t.Fatal("the switch does not round-trip a scope")
	}
	root := t.TempDir()
	a := newTestApp(t, root)
	a.reload()
	a.searchBox.open = true
	a.searchBox.query = "alpha"
	a.searchBox.scope = scopeFolder
	a.searchBoxFrame()
	if a.searchBox.walkedScope != scopeFolder {
		t.Fatalf("the walk recorded scope %v", a.searchBox.walkedScope)
	}
	// Switching the scope restarts the walk for the same query.
	a.searchBox.scope = scopeGlobal
	a.searchBoxFrame()
	if a.searchBox.walkedScope != scopeGlobal || a.searchBox.walked != "alpha" {
		t.Fatalf("scope change did not restart the walk: %q in %v", a.searchBox.walked, a.searchBox.walkedScope)
	}
	a.searchBoxClose() // the whole-machine walk stops with the box
}

// TestSearchBoxListsHitsByName: the list is in Everything's order — by name —
// whatever order the walk found them in.
func TestSearchBoxListsHitsByName(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	// Created out of order, so a walk that just appends cannot pass by luck.
	for _, name := range []string{"delta.txt", "Alpha.txt", "bravo.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("txt")
	tt.Frame()
	waitSearchBox(t, tt, a)
	got := namesOf(a.searchBox.results)
	want := []string{"Alpha.txt", "bravo.txt", "delta.txt", "f000.txt"}
	if len(got) != len(want) {
		t.Fatalf("the box lists %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the box lists %v, want %v", got, want)
		}
	}
}

// TestSearchBoxCursorStaysOnItsHit: a batch that sorts above the cursor moves
// the rows but not the choice, and a cursor the user never moved stays on the
// first name as hits arrive above it.
func TestSearchBoxCursorStaysOnItsHit(t *testing.T) {
	b := &searchBoxState{}
	b.addHits([]Entry{{Name: "bravo", Path: "/b"}, {Name: "delta", Path: "/d"}})
	if b.cursor != 0 || hitNames(b.results)[0] != "bravo" {
		t.Fatalf("the list starts at %v with the cursor on %d", hitNames(b.results), b.cursor)
	}
	// Nothing chose a row yet: the first name stays chosen as hits sort in.
	b.addHits([]Entry{{Name: "alpha", Path: "/a"}})
	if got := hitNames(b.results); got[0] != "alpha" || b.cursor != 0 {
		t.Fatalf("an unchosen cursor left the first name: row %d of %v", b.cursor, got)
	}
	// The user walks down to delta; the next batch keeps the cursor on it.
	b.cursor, b.cursorPath = 2, "/d"
	b.addHits([]Entry{{Name: "aaa", Path: "/aaa"}})
	if got := hitNames(b.results); got[0] != "aaa" || got[3] != "delta" {
		t.Fatalf("the batch did not sort into place: %v", got)
	}
	if b.cursor != 3 || b.results[b.cursor].Path != "/d" {
		t.Fatalf("the cursor moved off its hit: row %d of %v", b.cursor, hitNames(b.results))
	}
	// A hit the list no longer holds leaves the cursor at the top.
	b.cursorPath = "/gone"
	if row := b.cursorRow(); row != 0 || b.cursorPath != "" {
		t.Fatalf("a lost hit left the cursor at %d with path %q", row, b.cursorPath)
	}
}

// hitNames lists hits by name, the palette's own order.
func hitNames(hits []Entry) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}

// TestSearchCapNote: a list that stopped at the walker's cap says so.
func TestSearchCapNote(t *testing.T) {
	if note := searchCapNote(3); note != "" {
		t.Fatalf("a short list carries the note %q", note)
	}
	if note := searchCapNote(searchMaxResults); !strings.Contains(note, itoa(searchMaxResults)) {
		t.Fatalf("the cap note is %q, want the cap in it", note)
	}
}

// TestSearchHotkeyChord pins what the settings recorder stores and what
// the row then shows: a bare letter is refused, a chord round-trips, and
// the default reads as Command on macOS and Control elsewhere.
func TestSearchHotkeyChord(t *testing.T) {
	if _, ok := accelFromKey(0, ui.KeyK); ok {
		t.Fatal("a bare letter was accepted as a shortcut")
	}
	acc, ok := accelFromKey(ui.Ctrl|ui.Shift, ui.KeyK)
	if !ok || acc != "Ctrl+Shift+K" {
		t.Fatalf("chord = %q, ok %v", acc, ok)
	}
	mods, key, ok := parseSearchAccel(acc)
	if !ok || mods != ui.Ctrl|ui.Shift || key != ui.KeyK {
		t.Fatalf("parsed %q as mods %v key %v", acc, mods, key)
	}
	f, ok := accelFromKey(0, ui.KeyF5)
	if !ok || f != "F5" {
		t.Fatalf("function key = %q, ok %v", f, ok)
	}
	label := shortcutLabel("")
	if runtime.GOOS == "darwin" {
		if label != "⌘K" {
			t.Fatalf("default label = %q, want ⌘K", label)
		}
	} else if label != "Ctrl+K" {
		t.Fatalf("default label = %q, want Ctrl+K", label)
	}
}
