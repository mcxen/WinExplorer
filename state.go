package main

import (
	"time"
)

// tabState holds the navigation and listing of one Explorer tab. It is the
// state a.path, a.entries, a.selected, ... used to belong to the app; moving
// them here lets several tabs share the window.
type tabState struct {
	path    string
	entries []Entry
	loadErr string

	history  []string
	histIdx  int
	selected map[string]bool
	anchor   string

	query string

	renaming    string
	renameBuf   string
	editingPath bool
	pathDraft   string
}

// App holds the window chrome state and the tab collection. The active tab's
// fields are reached through cur() so the rest of the code reads a.path as
// before.
type App struct {
	tabs      []*tabState
	activeTab int

	sortKey    SortKey
	sortAsc    bool
	mode       ViewMode
	showHidden bool

	clipPaths []string
	clipCut   bool

	navWidth float32

	// statusMsg is shown by the status bar until the next navigation.
	statusMsg string
}

type ViewMode int

const (
	ViewDetails ViewMode = iota
	ViewList
	ViewSmallIcons
	ViewMediumIcons
	ViewLargeIcons
	ViewExtraLarge
)

type SortKey int

const (
	SortName SortKey = iota
	SortDate
	SortType
	SortSize
)

type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Ext     string // lower, no dot; empty for dirs
}

type Crumb struct{ Name, Path string }
type Place struct{ Name, Path, Kind string }

const pathThisPC = "::{this-pc}"

func newTab(path string) *tabState {
	return &tabState{
		path:     path,
		selected: map[string]bool{},
		history:  []string{path},
		histIdx:  0,
	}
}

// cur returns the active tab.
func (a *App) cur() *tabState { return a.tabs[a.activeTab] }

// allSelected gathers the selection across every tab, so a copy or cut made
// in one tab can be pasted after switching.
func (a *App) allSelected() []string {
	var out []string
	for _, t := range a.tabs {
		for p := range t.selected {
			out = append(out, p)
		}
	}
	return out
}
