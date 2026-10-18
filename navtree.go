package main

import (
	"os"
	"path/filepath"
	"sort"
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
	// synced is the path the tree last expanded to, and reveal that path's row
	// is to be scrolled into view. row is where the current location sits in the
	// pane's list — the pane is virtualized, so its place in the list is all it
	// takes to show it — and list is the pane's scrolling.
	synced string
	reveal string
	row    int
	list   ui.ListState
}

// navIndent is a tree row's left padding, a step per level.
func navIndent(depth int) float32 {
	return min(24+float32(depth)*navTreeStep, navTreeMaxIndent)
}

// navChildren is the folders under path, read once and kept: folders only, as
// the pane's tree lists them, in name order, hidden ones per 显示 > 隐藏的项目,
// symlinks to folders counted as folders. An unreadable folder has none, rather
// than an error the pane would have to draw.
func (a *App) navChildren(path string) []Entry {
	if kids, ok := a.navTree.kids[path]; ok {
		return kids
	}
	if a.navTree.kids == nil {
		a.navTree.kids = map[string][]Entry{}
	}
	var kids []Entry
	if entries, err := os.ReadDir(path); err == nil {
		for _, d := range entries {
			name := d.Name()
			if strings.HasPrefix(name, ".") && !a.showHidden {
				continue
			}
			dir := d.IsDir()
			if !dir && d.Type()&os.ModeSymlink != 0 {
				if st, err := os.Stat(filepath.Join(path, name)); err == nil && st.IsDir() {
					dir = true
				}
			}
			if !dir {
				continue
			}
			kids = append(kids, Entry{Name: name, Path: filepath.Join(path, name), IsDir: true})
		}
		sort.SliceStable(kids, func(i, j int) bool {
			return strings.ToLower(kids[i].Name) < strings.ToLower(kids[j].Name)
		})
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
}

// navChildrenClear forgets every read: 刷新 asks for the world as it is now.
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
	if a.navTree.open == nil {
		a.navTree.open = map[string]bool{}
	}
	root := volumeRootOf(path)
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		a.navTree.open[dir] = true
		if dir == root || dir == filepath.Dir(dir) {
			return
		}
	}
}

// volumeRootOf is the mounted volume path sits under, or "/" when none leads to
// it: the tree climbs to it and no further.
func volumeRootOf(path string) string {
	best := "/"
	for _, v := range sysVolumes() {
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
	if specialPath(path) {
		a.navTree.reveal = ""
		return
	}
	a.navTree.reveal = path
	a.navSync(path)
}

// navTreeReveal shows the current folder's row when the tab moved there: the
// pane's list keeps only what shows, so a row at the end of a long tree is
// scrolled to by its place in that list — centered, the way a tree usually puts
// the place you are at. The request stands until the row is in sight: the pane
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
	a.navTree.list.ScrollTo(a.navTree.row, ui.Center)
}
