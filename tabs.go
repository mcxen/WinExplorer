package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// navigate loads path in the active tab, resetting its transient state and
// pushing it onto the tab's history.
func (a *App) navigate(path string) {
	t := a.cur()
	t.path = cleanNav(path)
	t.query = ""
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
	t.selected = map[string]bool{}
	t.anchor = ""
	t.renaming = ""
	t.renameBuf = ""
	t.editingPath = false
	t.pathDraft = ""
	a.reload()
	a.requestPreview()
}

// up opens the parent of the active tab's folder. A volume root or "/" goes to
// This PC; This PC itself has no parent.
func (a *App) up() {
	if a.cur().path == pathThisPC {
		return
	}
	a.navigate(parentPath(a.cur().path))
}

func (a *App) canUp() bool { return a.cur().path != pathThisPC }

// reload lists the active tab's directory or This PC places, and clears the
// transient status message on success.
func (a *App) reload() {
	t := a.cur()
	a.statusMsg = ""
	t.loadErr = ""
	if t.path == pathThisPC {
		places := a.thisPCPlaces()
		entries := make([]Entry, len(places))
		for i, p := range places {
			entries[i] = Entry{Name: p.Name, Path: p.Path, IsDir: true}
		}
		t.entries = entries
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
	t.entries = entries
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
	a.requestPreview()
}

func (a *App) switchTo(i int) {
	if i >= 0 && i < len(a.tabs) && i != a.activeTab {
		a.activeTab = i
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
