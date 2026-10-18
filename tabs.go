package main

import (
	"os"

	"path/filepath"
	"slices"
	"strings"
	"sunvale/diskhealth"
)

// navigate loads path in the active tab, resetting its transient state and
// pushing it onto the tab's history.
func (a *App) navigate(path string) {
	t := a.cur()
	t.path = cleanNav(path)
	t.query = ""
	t.search = searchState{}
	t.selected = map[string]bool{}
	t.anchor = ""
	t.renaming = ""
	t.renameBuf = ""
	t.editingPath = false
	t.pathDraft = ""
	if t.histIdx < 0 || t.histIdx >= len(t.history) || t.history[t.histIdx] != t.path {
		if t.histIdx+1 < len(t.history) {
			t.history = slices.Clone(t.history[:t.histIdx+1])
		}
		t.history = append(t.history, t.path)
		t.histIdx = len(t.history) - 1
	}
	// Leaving the 磁盘健康 page stops its background probe, so a window only
	// polls disks while the page is on screen.
	if t.path != pathDisks {
		a.disksUnwatch()
	}
	a.reload()
	a.requestPreview()
}

func (a *App) canBack() bool { return a.cur().histIdx > 0 }

func (a *App) canForward() bool {
	t := a.cur()
	return t.histIdx >= 0 && t.histIdx+1 < len(t.history)
}

func (a *App) back() {
	t := a.cur()
	if t.histIdx <= 0 {
		return
	}
	t.histIdx--
	a.restoreHistory()
}

func (a *App) forward() {
	t := a.cur()
	if t.histIdx < 0 || t.histIdx+1 >= len(t.history) {
		return
	}
	t.histIdx++
	a.restoreHistory()
}

func (a *App) restoreHistory() {
	t := a.cur()
	t.path = t.history[t.histIdx]
	t.query = ""
	t.search = searchState{}
	t.selected = map[string]bool{}
	t.anchor = ""
	t.renaming = ""
	t.renameBuf = ""
	t.editingPath = false
	t.pathDraft = ""
	if t.path != pathDisks {
		a.disksUnwatch()
	}
	a.reload()
	a.requestPreview()
}

// up opens the parent of the active tab's folder. A volume root or "/" goes to
// This PC; This PC itself has no parent.
func (a *App) up() {
	switch a.cur().path {
	case pathThisPC, pathHome, pathGallery, pathTrash, pathDisks:
		return
	}
	a.navigate(parentPath(a.cur().path))
}

func (a *App) canUp() bool {
	switch a.cur().path {
	case pathThisPC, pathHome, pathGallery, pathTrash, pathDisks:
		return false
	}
	return true
}

// reload lists the active tab's directory or This PC places, and clears the
// transient status message on success.
func (a *App) reload() {
	t := a.cur()
	// The folder's listing is being read again: the tree's copy of it is stale.
	a.navChildrenDrop(t.path)
	a.statusMsg = ""
	t.loadErr = ""
	switch t.path {
	case pathThisPC:
		places := a.thisPCPlaces()
		entries := make([]Entry, len(places))
		for i, p := range places {
			e := Entry{Name: p.Name, Path: p.Path, IsDir: true}
			if p.Kind == "drive" {
				if free, total, ok := volumeCap(p.Path); ok {
					e.VolFree, e.VolTotal = free, total
				}
			}
			entries[i] = e
		}
		t.entries = entries
		return
	case pathHome:
		pins, recents := a.homeEntries()
		t.entries = append(pins, recents...)
		return
	case pathTrash:
		t.entries = trashEntries()
		return
	case pathGallery:
		t.entries = nil // the gallery renders from its own scan
		refreshGallery()
		return
	case pathDisks:
		t.entries = nil // the disk page renders from its own probe
		diskhealth.Refresh()
		return
	}
	dir, err := os.ReadDir(t.path)
	if err != nil {
		t.entries = nil
		t.loadErr = err.Error()
		return
	}
	entries := make([]Entry, 0, len(dir))
	for _, d := range dir {
		path := filepath.Join(t.path, d.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		isDir := info.IsDir()
		if !isDir && info.Mode()&os.ModeSymlink != 0 {
			if st, err := os.Stat(path); err == nil && st.IsDir() {
				isDir = true
			}
		}
		e := Entry{
			Name:    d.Name(),
			Path:    path,
			IsDir:   isDir,
			ModTime: info.ModTime(),
		}
		if !isDir {
			e.Size = info.Size()
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		}
		entries = append(entries, e)
	}
	// Folder sizes come from the background engine (sizes.go): a cached
	// tally shows at once, a folder whose ModTime has moved since its tally
	// is queued again, and an unseen folder is queued for its first walk.
	// The special pages above keep their own numbers.
	for i := range entries {
		e := &entries[i]
		if !e.IsDir {
			continue
		}
		if st, ok := lookupDirStat(e.Path); ok {
			e.Size = st.Size
			if !st.Mod.Equal(e.ModTime) {
				enqueueDirStat(e.Path)
			}
		} else {
			enqueueDirStat(e.Path)
		}
	}
	t.entries = entries
	// The listing the user is looking at is the freshest word on this folder:
	// the search index folds it in, so a file created, renamed or deleted here
	// is searchable from Ctrl+K at once (searchindex.go).
	searchIndexNote(t.path, entries)
}

func (a *App) toggleSort(key SortKey) {
	if a.sortKey == key {
		a.sortAsc = !a.sortAsc
		return
	}
	a.sortKey = key
	a.sortAsc = true
}

func (a *App) setMode(mode ViewMode) { a.mode = mode }

// iconZoomSteps are the 放大/缩小 stops. 1 is the mode's own size and 4 the
// largest, so 超大图标 reaches 384 px — big enough to read a photo by.
var iconZoomSteps = []float32{1, 1.25, 1.5, 2, 2.5, 3, 4}

// zoomStep is the stop the current factor sits on.
func (a *App) zoomStep() int {
	for i, z := range iconZoomSteps {
		if z >= a.iconZoom {
			return i
		}
	}
	return len(iconZoomSteps) - 1
}

func (a *App) canZoomIn() bool {
	return !isIconMode(a.mode) || a.zoomStep() < len(iconZoomSteps)-1
}

func (a *App) canZoomOut() bool {
	return !isIconMode(a.mode) || a.zoomStep() > 0
}

// zoomIn enlarges the icon tiles one stop. From 详细信息 or 列表 it opens
// the icon view instead, where there is something to enlarge.
func (a *App) zoomIn() {
	if !isIconMode(a.mode) {
		a.mode = ViewMediumIcons
		a.iconZoom = 1
		return
	}
	if i := a.zoomStep(); i+1 < len(iconZoomSteps) {
		a.iconZoom = iconZoomSteps[i+1]
	}
}

// zoomOut shrinks the icon tiles one stop, or opens the small icon view
// from 详细信息 or 列表.
func (a *App) zoomOut() {
	if !isIconMode(a.mode) {
		a.mode = ViewSmallIcons
		a.iconZoom = 1
		return
	}
	if i := a.zoomStep(); i > 0 {
		a.iconZoom = iconZoomSteps[i-1]
	}
}

func (a *App) toggleHidden() { a.showHidden = !a.showHidden }

// openTab opens path in a new tab and makes it active, mirroring Windows 11
// Explorer's Ctrl+T.
func (a *App) openTab(path string) {
	t := newTab(cleanNav(path))
	a.tabs = append(a.tabs, t)
	a.activeTab = len(a.tabs) - 1
	a.reload()
}

// closeTab removes the tab at i. The active index moves to a neighbour so the
// window never shows a nil tab.
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.tabs) || len(a.tabs) <= 1 {
		return
	}
	a.tabs = slices.Delete(a.tabs, i, i+1)
	if a.activeTab >= len(a.tabs) {
		a.activeTab = len(a.tabs) - 1
	} else if a.activeTab > i {
		a.activeTab--
	}
	if a.cur().path != pathDisks {
		a.disksUnwatch()
	}
	a.requestPreview()
}

func (a *App) switchTo(i int) {
	if i >= 0 && i < len(a.tabs) && i != a.activeTab {
		a.activeTab = i
		if a.cur().path != pathDisks {
			a.disksUnwatch()
		}
		a.requestPreview()
	}
}

// closeOthers keeps only the tab at i and makes it active, as Explorer's
// tab menu's 关闭其他标签页.
func (a *App) closeOthers(keep int) {
	if keep < 0 || keep >= len(a.tabs) || len(a.tabs) <= 1 {
		return
	}
	a.tabs = []*tabState{a.tabs[keep]}
	a.activeTab = 0
	a.requestPreview()
}

// closeRight removes every tab after i, clamping the active index, as
// Explorer's tab menu's 关闭右侧标签页.
func (a *App) closeRight(from int) {
	if from < 0 || from >= len(a.tabs)-1 {
		return
	}
	a.tabs = a.tabs[:from+1]
	if a.activeTab > from {
		a.activeTab = from
		a.requestPreview()
	}
}
