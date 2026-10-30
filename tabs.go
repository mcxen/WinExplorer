package main

import (
	"slices"
	"sunvale/diskhealth"
	"sunvale/internal/netfs"
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
	a.store.pushVisit(t.path)
	a.reload()
	a.selectAfterNav(t)
	a.requestPreview()
}

// selectAfterNav applies the pending selection a backward move left: 上一级
// and 后退 both land on the folder that was just left, so it should be
// selected there, as Explorer does. The path is only taken when the listing
// really holds it — a folder deleted meanwhile leaves nothing selected.
func (a *App) selectAfterNav(t *tabState) {
	if t.selectPath == "" {
		return
	}
	path := t.selectPath
	t.selectPath = ""
	for _, e := range t.entries {
		if e.Path == path {
			a.selectOnly(path)
			// The row may sit below the fold in a long listing: raise it
			// into view the way a keyboard move does.
			t.reveal = path
			return
		}
	}
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
	// 后退 lands on the folder just left; select it there, as 上一级 does.
	t.selectPath = t.path
	t.histIdx--
	a.restoreHistory()
}

func (a *App) forward() {
	t := a.cur()
	if t.histIdx < 0 || t.histIdx+1 >= len(t.history) {
		return
	}
	t.selectPath = t.path
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
	a.selectAfterNav(t)
	a.requestPreview()
}

// up opens the parent of the active tab's folder, with the folder left
// behind selected there — Explorer's behavior, so going up and back down is
// one click. A volume root or "/" goes to This PC; This PC itself has no
// parent.
func (a *App) up() {
	t := a.cur()
	switch t.path {
	case pathThisPC, pathHome, pathGallery, pathTrash, pathDisks:
		return
	}
	// The listing is rebuilt by navigate, which selects this row once the
	// parent's entries are in hand.
	t.selectPath = t.path
	a.navigate(parentPath(t.path))
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
	aiFx.forgetLeft()
	t := a.cur()
	// Warm the read-only decision off the view path: the command bar asks
	// every frame whether this folder lives inside an archive.
	pathInArchive(t.path)
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
	pathInArchive(t.path) // fills the cache inArchive reads during view
	entries, err := entryLister(t.path).List(t.path)
	if err != nil {
		t.entries = nil
		t.loadErr = err.Error()
		return
	}
	a.decorateEntries(entries)
	t.entries = entries
}

// decorateEntries fills in what the lister does not: a folder's size from
// the background engine (sizes.go), and the search index's copy of the
// listing. A network folder is queued for a size walk only when the cached
// tally is there and stale — the first look at a share does not start a
// walk over its subtree, which on a far filesystem costs far more than the
// number is worth; 刷新统计 asks for one explicitly.
func (a *App) decorateEntries(entries []Entry) {
	t := a.cur()
	network := netfs.IsNetworkPath(t.path)
	t.networkDir = network
	t.networkWarm = network && a.store.settings().networkWarmup()
	for i := range entries {
		e := &entries[i]
		if !e.IsDir {
			continue
		}
		st, ok := lookupDirStat(e.Path)
		if ok {
			e.Size = st.Size
			if !st.Mod.Equal(e.ModTime) {
				enqueueDirStat(e.Path)
			}
			continue
		}
		if !network {
			enqueueDirStat(e.Path)
		}
	}
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

// maxClosedTabs is how many closed tabs 重新打开关闭的标签页 (Ctrl+Shift+T)
// can bring back, newest first.
const maxClosedTabs = 20

// closedTab is what reopening a tab needs: where it was in the strip, the
// folder it showed and its 后退/前进 history.
type closedTab struct {
	index   int
	history []string
	histIdx int
	path    string
}

// openTab opens path in a new tab at the strip's end and makes it active,
// mirroring Windows 11 Explorer's Ctrl+T.
func (a *App) openTab(path string) {
	a.insertTab(len(a.tabs), newTab(cleanNav(path)))
}

// duplicateTab opens a copy of the tab at i right after it, with its
// history, as Explorer's 复制标签页: 后退 in the copy goes where the
// original's did.
func (a *App) duplicateTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	src := a.tabs[i]
	t := newTab(src.path)
	t.history = slices.Clone(src.history)
	t.histIdx = src.histIdx
	a.insertTab(i+1, t)
}

// insertTab puts t into the strip at i and brings it to the front.
func (a *App) insertTab(i int, t *tabState) {
	i = min(max(i, 0), len(a.tabs))
	a.tabs = slices.Insert(a.tabs, i, t)
	if a.activeTab >= i && len(a.tabs) > 1 {
		a.activeTab++ // the old front tab moved one place right
	}
	a.activate(i)
}

// activate makes the tab at i the front one. The tab going back commits its
// inline rename and drops its address-bar draft; the new front tab's listing
// is read again, since a tab in the back can go stale (a drop onto its tab,
// an operation in another tab) and a restored tab has never been listed.
func (a *App) activate(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	var leaveErr error
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) && a.activeTab != i {
		leaveErr = a.leaveTab(a.tabs[a.activeTab])
	}
	a.activeTab = i
	a.tabReveal = true
	if a.cur().path != pathDisks {
		a.disksUnwatch()
	}
	sel := a.cur().selected
	a.reload()
	// reload keeps the selection map, but a selected file that is gone
	// meanwhile must not stay selected out of sight.
	a.pruneSelection(sel)
	a.requestPreview()
	a.renameStatus(leaveErr)
}

// leaveTab settles the transient editors of a tab going away. Explorer commits
// an inline rename when focus leaves it; a bad target reports the same outcome
// as Enter but never holds up the tab switch or close.
func (a *App) leaveTab(t *tabState) error {
	err := a.commitRenameIn(t)
	t.editingPath, t.pathDraft = false, ""
	t.marquee = marqueeState{}
	return err
}

// pruneSelection keeps only the selected paths the fresh listing still has.
func (a *App) pruneSelection(sel map[string]bool) {
	t := a.cur()
	if len(sel) == 0 || specialPath(t.path) {
		return
	}
	have := make(map[string]bool, len(t.entries))
	for _, e := range t.entries {
		have[e.Path] = true
	}
	for p := range sel {
		if !have[p] {
			delete(sel, p)
		}
	}
	if !have[t.anchor] {
		t.anchor = ""
	}
}

// rememberClosed pushes the tab at i onto the reopen stack.
func (a *App) rememberClosed(i int) {
	t := a.tabs[i]
	a.closedTabs = append(a.closedTabs, closedTab{
		index: i, path: t.path, history: slices.Clone(t.history), histIdx: t.histIdx,
	})
	if n := len(a.closedTabs); n > maxClosedTabs {
		a.closedTabs = slices.Delete(a.closedTabs, 0, n-maxClosedTabs)
	}
}

// closeTab removes the tab at i. Closing the front tab brings forward the
// one that slides into its place — its right neighbour, or its left one
// when it was the last — as Explorer and the browsers do. The last tab
// stays: a window always shows a folder.
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.tabs) || len(a.tabs) <= 1 {
		return
	}
	leaveErr := a.leaveTab(a.tabs[i])
	a.tabs[i].search.stop()
	a.rememberClosed(i)
	wasActive := i == a.activeTab
	a.tabs = slices.Delete(a.tabs, i, i+1)
	switch {
	case wasActive:
		a.activeTab = -1 // the tab left; nothing to tidy on it
		a.activate(min(i, len(a.tabs)-1))
	case a.activeTab > i:
		a.activeTab--
	}
	a.renameStatus(leaveErr)
}

// closeTabByID closes the tab t, wherever it sits now: input handlers run
// outside the frame's index loop, so they name the tab rather than a place.
func (a *App) closeTabByID(id int) {
	if i := a.tabIndex(id); i >= 0 {
		a.closeTab(i)
	}
}

// tabIndex is where the tab with id sits, or -1.
func (a *App) tabIndex(id int) int {
	for i, t := range a.tabs {
		if t.id == id {
			return i
		}
	}
	return -1
}

// reopenClosedTab brings back the most recently closed tab where it was,
// with its history, as Ctrl+Shift+T. A folder that is gone meanwhile
// opens 主文件夹, as a restored session's tab does.
func (a *App) reopenClosedTab() {
	n := len(a.closedTabs)
	if n == 0 {
		return
	}
	ct := a.closedTabs[n-1]
	a.closedTabs = a.closedTabs[:n-1]
	t := newTab(restoreTabPath(ct.path))
	if t.path == ct.path && len(ct.history) > 0 {
		t.history = ct.history
		t.histIdx = min(max(ct.histIdx, 0), len(ct.history)-1)
	}
	a.insertTab(ct.index, t)
}

func (a *App) switchTo(i int) {
	if i >= 0 && i < len(a.tabs) && i != a.activeTab {
		a.activate(i)
	}
}

// closeOthers keeps only the tab at i and makes it active, as Explorer's
// tab menu's 关闭其他标签页. The closed tabs can be reopened one by one.
func (a *App) closeOthers(keep int) {
	if keep < 0 || keep >= len(a.tabs) || len(a.tabs) <= 1 {
		return
	}
	// Pushed right to left, so reopening one by one puts each back in its
	// own place.
	for i := len(a.tabs) - 1; i >= 0; i-- {
		if i != keep {
			a.rememberClosed(i)
		}
	}
	front := keep == a.activeTab
	a.tabs = []*tabState{a.tabs[keep]}
	if front {
		a.activeTab = 0
		return
	}
	a.activeTab = -1
	a.activate(0)
}

// closeRight removes every tab after i, as Explorer's tab menu's
// 关闭右侧标签页. When the front tab was among them, tab i comes forward.
func (a *App) closeRight(from int) {
	if from < 0 || from >= len(a.tabs)-1 {
		return
	}
	for i := len(a.tabs) - 1; i > from; i-- {
		a.rememberClosed(i)
	}
	a.tabs = a.tabs[:from+1]
	if a.activeTab > from {
		a.activeTab = -1
		a.activate(from)
	}
}

// moveTab moves the tab at from to the place to, the front tab staying the
// front tab wherever it ends up, as dragging a tab along the strip does.
func (a *App) moveTab(from, to int) {
	if from < 0 || from >= len(a.tabs) || to < 0 || to >= len(a.tabs) || from == to {
		return
	}
	front := a.tabs[a.activeTab]
	t := a.tabs[from]
	a.tabs = slices.Delete(a.tabs, from, from+1)
	a.tabs = slices.Insert(a.tabs, to, t)
	a.activeTab = slices.Index(a.tabs, front)
}
