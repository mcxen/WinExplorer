package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// navtree.go is the navigation pane's folder tree: the volumes under 此电脑 open
// into their folders a level at a time, and the tree follows the tab — after
// every navigation the current folder's ancestors are opened and its row is
// selected and scrolled into view, the way Explorer's 「展开到当前文件夹」 does.
//
// Nothing is walked ahead of time: a folder's children are read when it first
// opens, and kept until something says otherwise (a reload drops the folder the
// pane just re-read). A volume therefore costs the pane one directory read per
// level the user actually looks at, and a deep tree costs one per level the
// current folder sits under.

const (
	// navTreeStep is a level's indent, and navTreeMaxIndent the most of it a
	// deep tree may take: past that the labels would be pushed out of the pane.
	navTreeStep      float32 = 14
	navTreeMaxIndent float32 = 96
)

// navTree is the tree's state: which folders are open, the children read for
// them, where the tree last synced to, and the scrolling that reveals a row.
type navTree struct {
	open map[string]bool
	kids map[string][]Entry
	// focused and cursor are the keyboard's place in the pane. cursor is a
	// row key rather than a path because fixed places can name the same path
	// as a tree folder.
	focused bool
	cursor  string
	// hover and activeDepth drive the indent guides' fade and their active
	// ancestor chain.
	hover       bool
	activeDepth int
	// synced is the path the tree last expanded to, and reveal that path's row
	// is to be scrolled into view. row is where the current location sits in the
	// pane's list — the pane is virtualized, so its place in the list is all it
	// takes to show it — and list is the pane's scrolling.
	synced string
	reveal string
	// auto holds the folders navSync opened (not the user): the next follow
	// closes those off the new chain.
	auto map[string]bool
	row  int
	list ui.ListState
	// clicked is the key of the pane row the user clicked, and clickedPath
	// where it went: that row, not a twin elsewhere in the pane, is the one
	// selected while the tab stays there. noSync is a place reached from
	// a row above the tree, which the tree does not open itself up to.
	clicked     string
	clickedPath string
	noSync      string
	// recent is 最近位置 in the order it holds for this window, and
	// recentIn the same set for lookups (sidebar.go recentPlaceRows).
	recent   []string
	recentIn map[string]bool
}

// navFromPane notes that a click on the pane row key is taking the tab to
// path. tree says whether the row is one of the tree's: a pinned or recent
// row does not open the tree down to its folder.
func (a *App) navFromPane(key, path string, tree bool) {
	a.navTreeFocus(key)
	a.navTree.clicked = key
	a.navTree.clickedPath = cleanNav(path)
	a.navTree.noSync = ""
	if !tree {
		a.navTree.noSync = cleanNav(path)
	}
}

// navTreeFocus gives the pane's keyboard cursor to a clicked row. The row is
// focused without changing the tab; navigation remains an explicit action.
func (a *App) navTreeFocus(key string) {
	a.navTree.focused = true
	a.navTree.cursor = key
}

// navIndent is a tree row's left padding, a step per level.
func navIndent(depth int) float32 {
	return min(24+float32(depth)*navTreeStep, navTreeMaxIndent)
}

// navChildren is the folders under path, read once and kept: folders only, as
// the pane's tree lists them, in name order, hidden ones per 显示 > 隐藏的项目,
// symlinks to folders counted as folders. An unreadable folder has none, rather
// than an error the pane would have to draw.
//
// The first read is a directory walk, so it never runs inside a view: the
// pane's shared folder-name cache reads it on a goroutine (pathinput.go) and
// the row appears on the following frame. The built children are kept for the
// life of the tree, as before.
func (a *App) navChildren(path string) []Entry {
	if kids, ok := a.navTree.kids[path]; ok {
		return kids
	}
	var d dirNames
	if inView.Load() > 0 {
		d = folderNames.get(path)
		if !d.ready {
			return nil // on its way; the next frame fills the row
		}
	} else {
		d = folderNames.fresh(path)
	}
	if a.navTree.kids == nil {
		a.navTree.kids = map[string][]Entry{}
	}
	kids := make([]Entry, 0, len(d.names))
	for _, name := range d.names {
		if !a.showHidden && d.hidden[name] {
			continue
		}
		kids = append(kids, Entry{Name: name, Path: filepath.Join(path, name), IsDir: true})
	}
	a.navTree.kids[path] = kids
	return kids
}

// navChildrenDrop forgets the children read for path: its listing changed, so
// the next look reads it again.
func (a *App) navChildrenDrop(path string) {
	if a.navTree.kids != nil {
		delete(a.navTree.kids, path)
	}
	folderNames.forget(path)
}

// navChildrenClear forgets every read: 刷新 asks for the world as it is now.
// The shared folder-name cache is left alone: 显示隐藏的项目 only needs the
// reads filtered again, and doRefresh drops that cache separately.
func (a *App) navChildrenClear() {
	a.navTree.kids = nil
}

// navSync opens every folder above path, up to the volume it sits on: the tree
// then shows the tab's folder without walking anything the user did not ask to
// see. The climb stops at the volume root — a folder on an external volume is
// shown under that volume, not under the /Volumes entry the boot volume also
// lists — and special pages have no place in the tree.
func (a *App) navSync(path string) {
	if path == "" || specialPath(path) {
		return
	}
	// 主文件夹 and its known folders have rows of their own above the tree,
	// and that row lights up: opening every system folder from / down to
	// the home folder would bury the pane under bin, boot, dev and etc.
	if underKnownFolder(path) {
		return
	}
	if a.navTree.open == nil {
		a.navTree.open = map[string]bool{}
	}
	// Only the chain down to the current folder stays open: the ancestors a
	// previous follow opened close again unless they are on the new chain,
	// so walking around never leaves a long trail of opened folders. Folders
	// the user opened by hand are not in auto and are left as they are.
	chain := map[string]bool{}
	root := volumeRootOf(path, a.thisPCPlaces())
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		chain[dir] = true
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
	}
	for dir := range a.navTree.auto {
		if !chain[dir] {
			delete(a.navTree.open, dir)
			delete(a.navTree.auto, dir)
		}
	}
	if a.navTree.auto == nil {
		a.navTree.auto = map[string]bool{}
	}
	for dir := range chain {
		if !a.navTree.open[dir] {
			a.navTree.open[dir] = true
			a.navTree.auto[dir] = true
		}
	}
}

// volumeRootOf is the mounted volume path sits under, or "/" when none leads to
// it: the tree climbs to it and no further.
func volumeRootOf(path string, places []Place) string {
	best := "/"
	for _, v := range places {
		if v.Path == "" || v.Path == "/" {
			continue
		}
		base := strings.TrimSuffix(v.Path, "/")
		if path != base && !strings.HasPrefix(path, base+"/") {
			continue
		}
		if len(v.Path) > len(best) {
			best = v.Path
		}
	}
	return best
}

// navTreeFollow opens the current folder's ancestors when the tab moved to a
// new place, before the pane gathers its rows: the tree then lists where the tab
// is, and navTreeReveal scrolls to its row. A special page — 主页, 此电脑, 回收站,
// a plugin's page — has no folders of its own to open.
func (a *App) navTreeFollow() {
	path := a.cur().path
	if path == a.navTree.synced {
		return
	}
	a.navTree.synced = path
	if path != a.navTree.clickedPath {
		// Somewhere else took the tab here — the file pane, the address bar,
		// history — so no pane row is the one it came from.
		a.navTree.clicked, a.navTree.clickedPath = "", ""
	}
	if path == a.navTree.noSync {
		a.navTree.noSync = ""
		a.navTree.reveal = ""
		return
	}
	a.navTree.noSync = ""
	if specialPath(path) || a.treeNoExpand() {
		// 展开到打开的文件夹 off: the tree keeps the user's own opening.
		a.navTree.reveal = ""
		return
	}
	a.navTree.reveal = path
	a.navSync(path)
}

func (a *App) treeNoExpand() bool { return a.store != nil && !a.store.settings().treeExpand() }

// treeExpand reports 展开到打开的文件夹: off unless the user turned it on.
func (s appSettings) treeExpand() bool {
	return s.TreeExpand != nil && *s.TreeExpand && !s.TreeNoExpand
}

// navTreeReveal shows the current folder's row when the tab moved there: the
// pane's list keeps only what shows, so a row at the end of a long tree is
// scrolled to by its place in that list — as little as brings it in, so the
// rows the user was looking at move no further than they must. The request stands until the row is in sight: the pane
// may not list this place yet (此电脑 collapsed, or the list without a layout
// in its first frame), and it becomes showable — and shown — as soon as it does.
func (a *App) navTreeReveal() {
	if a.navTree.reveal == "" {
		return
	}
	if a.navTree.row < 0 {
		return // the pane does not list this place: yet, or at all
	}
	first, last := a.navTree.list.Visible()
	if a.navTree.row >= first && a.navTree.row <= last {
		a.navTree.reveal = "" // in sight: the reveal is done
		return
	}
	a.navTree.list.ScrollIntoView(a.navTree.row)
}

// underKnownFolder reports whether path is 主文件夹 or one of the known
// folders the sidebar lists (桌面, 下载, …), or inside one of the latter.
// The home folder itself only counts exactly: a project somewhere under it
// still opens the tree down to it.
func underKnownFolder(path string) bool {
	path = filepath.Clean(path)
	home, _ := os.UserHomeDir()
	for dir := range knownFolderNames() {
		if dir == filepath.Clean(home) {
			if path == dir {
				return true
			}
			continue
		}
		if pathWithin(path, dir) {
			return true
		}
	}
	return false
}
