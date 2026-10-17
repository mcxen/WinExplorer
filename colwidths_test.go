package main

import "testing"

// TestColWidthsAccessors checks the width accessors: zero keeps each
// column's default, the user's widths win, and the 此电脑 page's size slot
// stays the drive capacity column.
func TestColWidthsAccessors(t *testing.T) {
	a := &App{tabs: []*tabState{newTab(pathHome)}, activeTab: 0}
	if a.dateColW() != colDateW || a.typColW() != colTypeW || a.locColW() != colLocW {
		t.Fatal("zero widths should keep the defaults")
	}
	if a.nameColW() != colNameFlexW {
		t.Fatalf("nameColW = %v, want the flex default", a.nameColW())
	}
	if a.sizeColW() != colSizeW {
		t.Fatalf("sizeColW = %v, want %v", a.sizeColW(), colSizeW)
	}
	a.cur().path = pathThisPC
	if a.sizeColW() != colDriveW {
		t.Fatalf("thisPC sizeColW = %v, want %v", a.sizeColW(), colDriveW)
	}
	a.cur().path = "/tmp/有宽度的文件夹"
	a.colWidths = colWidths{name: 300, date: 200, typ: 150, loc: 240, size: 130}
	if a.nameColW() != 300 || a.dateColW() != 200 || a.typColW() != 150 ||
		a.locColW() != 240 || a.sizeColW() != 130 {
		t.Fatalf("user widths should win: %+v", a.colWidths)
	}
}

// TestClampColW keeps dragged widths within a column's floor and ceiling.
func TestClampColW(t *testing.T) {
	if got := clampColW(10, 64, 720); got != 64 {
		t.Fatalf("clampColW(10) = %v, want 64", got)
	}
	if got := clampColW(9999, 64, 720); got != 720 {
		t.Fatalf("clampColW(9999) = %v, want 720", got)
	}
	if got := clampColW(300, 64, 720); got != 300 {
		t.Fatalf("clampColW(300) = %v, want 300", got)
	}
}

// TestSaveColWidthsPersists checks the widths write through state.json and
// the unset ones stay zero.
func TestSaveColWidthsPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	a.colWidths = colWidths{name: 300, date: 200, size: 130}
	a.saveColWidths()
	s := loadState().settings()
	if s.ColName != 300 || s.ColDate != 200 || s.ColSize != 130 {
		t.Fatalf("saved settings = %+v, want the widths", s)
	}
	if s.ColType != 0 || s.ColLoc != 0 {
		t.Fatalf("unset widths should stay zero, got %+v", s)
	}
	// Loading them back into a window keeps the same widths.
	a.loadPersistedSettings()
	if a.colWidths.name != 300 || a.colWidths.date != 200 || a.colWidths.size != 130 {
		t.Fatalf("loaded widths = %+v, want what was saved", a.colWidths)
	}
}
