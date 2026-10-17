package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func newStoreApp(t *testing.T, dir string) *App {
	t.Helper()
	a := newTestApp(t, dir)
	a.store = &persistedState{Pinned: defaultPins()}
	a.homePath = dir
	return a
}

// trashReadable reports whether this process may list the trash. macOS's
// TCC denies reads of ~/.Trash to terminals without Full Disk Access while
// renames into it still work, so tests that enumerate the trash degrade to
// log-and-Stat assertions when the listing is denied.
func trashReadable(t *testing.T) bool {
	t.Helper()
	dir, err := trashDir()
	if err != nil {
		return false
	}
	_, rerr := os.ReadDir(dir)
	return rerr == nil
}

// loggedTrashRecord asserts the fresh store's trash log knows exactly the
// one deletion and that the file is really in the trash under the logged
// name — no listing permission needed (see trashReadable) — and returns
// the record.
func loggedTrashRecord(t *testing.T, a *App, wantOriginal string) trashRecord {
	t.Helper()
	if len(a.store.TrashLog) != 1 {
		t.Fatalf("trash log = %d records, want 1", len(a.store.TrashLog))
	}
	rec := a.store.TrashLog[0]
	if rec.Original != wantOriginal || rec.TrashName == "" {
		t.Fatalf("trash log = %+v, want it to know %q", rec, wantOriginal)
	}
	trash, err := trashDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, rec.TrashName)); err != nil {
		t.Fatalf("logged trash item missing: %v", err)
	}
	return rec
}

func TestSpecialPaths(t *testing.T) {
	for _, p := range []string{pathThisPC, pathHome, pathTrash} {
		if !specialPath(p) {
			t.Fatalf("specialPath(%q) = false", p)
		}
	}
	if specialPath("/tmp") {
		t.Fatal("/tmp is not special")
	}
	if cleanNav("主文件夹") != pathHome || cleanNav("回收站") != pathTrash {
		t.Fatal("chinese aliases broken")
	}
}

func TestPinRecentState(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-state")
	defer os.RemoveAll(dir)
	a := newStoreApp(t, dir)

	pin := filepath.Join(dir, "pinme")
	os.MkdirAll(pin, 0o755)
	if !a.store.pin(pin) {
		t.Fatal("pin should add")
	}
	if a.store.pin(pin) {
		t.Fatal("double pin")
	}
	if !a.store.isPinned(pin) {
		t.Fatal("isPinned")
	}
	if got := a.pinnedPlaces(); len(got) == 0 || got[0].Path != pin {
		t.Fatalf("pinnedPlaces = %v", got)
	}
	if !a.store.unpin(pin) || a.store.isPinned(pin) {
		t.Fatal("unpin")
	}

	// Recents: newest first, deduplicated, capped.
	for i := 0; i < maxRecent+3; i++ {
		a.store.pushRecent(filepath.Join(dir, "f"+string(rune('a'+i%26))+string(rune('0'+i))))
	}
	recent := a.store.recentList()
	if len(recent) != maxRecent {
		t.Fatalf("recent = %d, want %d", len(recent), maxRecent)
	}
	if recent[0] != a.store.Recent[0] {
		t.Fatal("newest not first")
	}
	a.store.pushRecent(recent[3])
	if a.store.recentList()[0] != recent[3] {
		t.Fatal("re-push not moved to front")
	}
}

func TestHomeEntries(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-home")
	defer os.RemoveAll(dir)
	a := newStoreApp(t, dir)
	pin := filepath.Join(dir, "项目")
	os.MkdirAll(pin, 0o755)
	a.store.pin(pin)
	file := filepath.Join(dir, "note.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	a.recordRecent(file)

	pins, recents := a.homeEntries()
	if len(pins) == 0 || pins[0].Path != pin || pins[0].Name != "项目" {
		t.Fatalf("pins = %v", pins)
	}
	if len(recents) != 1 || recents[0].Path != file || recents[0].Dir != dir {
		t.Fatalf("recents = %v", recents)
	}
	// recordRecent ignores directories.
	a.recordRecent(pin)
	if len(a.store.recentList()) != 1 {
		t.Fatal("dir recorded as recent")
	}
}

func TestTrashRestoreAndUndo(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-trash")
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "工作")
	os.MkdirAll(dir, 0o755)
	victim := filepath.Join(dir, "doc.txt")
	os.WriteFile(victim, []byte("keep me"), 0o644)

	a := newStoreApp(t, dir)
	a.selectOnly(victim)
	a.doDelete()
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatal("victim not moved to trash")
	}
	if !a.canUndo() {
		t.Fatal("delete not recorded for undo")
	}
	if len(a.undo) != 1 {
		t.Fatalf("undo stack = %d", len(a.undo))
	}

	// 回收站 page lists it, and the restore log knows its origin. The log
	// itself is checked directly — the fresh store holds exactly one record
	// — and the file must be in the trash under the logged name. The
	// listing cross-check runs only when the trash is readable (macOS TCC).
	rec := loggedTrashRecord(t, a, victim)
	if trashReadable(t) {
		found := false
		for _, e := range trashEntries() {
			if e.Name == rec.TrashName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("victim %q missing from the trash listing", rec.TrashName)
		}
	}

	// Ctrl+Z puts it back where it was.
	a.doUndo()
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("undo did not restore: %v", err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep me" {
		t.Fatalf("content changed: %q", b)
	}
	if a.canUndo() {
		t.Fatal("undo stack not drained")
	}
	// Our item is gone from the trash; other system items may remain.
	trash, err := trashDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, rec.TrashName)); !os.IsNotExist(err) {
		t.Fatalf("restored item still in trash: %v", err)
	}
	if trashReadable(t) {
		for _, e := range trashEntries() {
			if e.Name == rec.TrashName {
				t.Fatalf("restored item still in trash: %q", e.Name)
			}
		}
	}
}

func TestUndoMoveAndCreate(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-undo")
	defer os.RemoveAll(root)
	dir := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	os.MkdirAll(dir, 0o755)
	os.MkdirAll(dst, 0o755)
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("m"), 0o644)
	a := newStoreApp(t, dir)

	// Drag-move then undo.
	a.doDropInto(0, dst, []string{f})
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("move failed: %v", err)
	}
	a.doUndo()
	if _, err := os.Stat(f); err != nil {
		t.Fatalf("undo did not move back: %v", err)
	}

	// New folder then undo.
	a.doNewFolder()
	made := filepath.Join(dir, "新建文件夹")
	if _, err := os.Stat(made); err != nil {
		t.Fatalf("new folder missing: %v", err)
	}
	a.doUndo()
	if _, err := os.Stat(made); !os.IsNotExist(err) {
		t.Fatal("undo left the new folder behind")
	}

	// Copy then undo removes only the copy.
	a.doDropInto(ui.Cmd, dst, []string{f})
	copyPath := filepath.Join(dst, "a.txt")
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	a.doUndo()
	if _, err := os.Stat(f); err != nil {
		t.Fatal("undo removed the original too")
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Fatal("undo left the copy behind")
	}
}

func TestRecursiveSearch(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-search")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "alpha.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "sub", "beta-alpha.md"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "sub", "deep", "gamma-ALPHA.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(root, ".git", "alpha-config"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "other.bin"), nil, 0o644)

	hits := walkMatches(root, "alpha", false, time.Now().Add(5*time.Second))
	var paths []string
	for _, h := range hits {
		paths = append(paths, filepath.Base(h.Path))
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %v", paths)
	}
	for _, h := range hits {
		if h.Dir == "" {
			t.Fatalf("hit without Dir: %v", h)
		}
		if !strings.Contains(strings.ToLower(h.Name), "alpha") {
			t.Fatalf("bad hit %q", h.Name)
		}
	}

	// Hidden folders are skipped unless asked.
	if h := walkMatches(root, "alpha-config", false, time.Now().Add(2*time.Second)); len(h) != 0 {
		t.Fatalf("hidden hit leaked: %v", h)
	}
	if h := walkMatches(root, "alpha-config", true, time.Now().Add(2*time.Second)); len(h) != 1 {
		t.Fatalf("showHidden search: %v", h)
	}
}

func TestGroupedRows(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-group")
	defer os.RemoveAll(dir)
	a := newStoreApp(t, dir)
	old := time.Now().AddDate(0, 0, -30)
	// groupedRows receives entries already sorted, as visibleEntries
	// returns them; sizes ascending.
	entries := []Entry{
		{Name: "a.md", Ext: "md", Size: 0, ModTime: time.Now()},
		{Name: "b.txt", Ext: "txt", Size: 10, ModTime: old},
		{Name: "c.bin", Ext: "bin", Size: 200 * 1024 * 1024, ModTime: old},
	}
	a.grouping = groupNone
	if rows := a.groupedRows(entries); len(rows) != 3 || rows[0].header != "" {
		t.Fatalf("ungrouped rows = %v", rows)
	}
	a.grouping = SortSize
	rows := a.groupedRows(entries)
	if len(rows) != 6 { // 3 entries + 3 headers
		t.Fatalf("grouped rows = %d: %+v", len(rows), rows)
	}
	headers := []string{rows[0].header, rows[2].header, rows[4].header}
	want := []string{"空", "小于 100 KB", "大"}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("header[%d] = %q want %q", i, headers[i], want[i])
		}
	}
	if rows[1].e.Name != "a.md" || rows[3].e.Name != "b.txt" || rows[5].e.Name != "c.bin" {
		t.Fatalf("entries not under their headers: %+v", rows)
	}
}
