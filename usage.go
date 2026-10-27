package main

import (
	"cmp"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// usage.go is the 可视化查看占用 plugin scan and squarified-treemap layout
// math. The dialog painting lives in usage_view.go (LAY1 split).

const (
	usagePluginID = "usage"
	usageTimeout  = 15 * time.Second // the walk's deadline, as search's
	usageWorkers  = 6                // concurrent folder reads
	usageMinShare = 0.004            // cells under 0.4% merge into 其他
	usageMaxCells = 400              // cells drawn; the rest merges into 其他
)

// usageMaxNodes is files and folders the scan collects before it stops —
// a var so tests can shrink it.
var usageMaxNodes int64 = 120000

var (
	// usageDlg, usageDrillAt and usagePickAt are the dialog's per-window
	// state (plugintask.go): the open flag, the drill position and the
	// picked cell.
	usageDlg     = appFlag{}
	usageDrillAt = appVal[*usageNode]{}
	usagePickAt  = appVal[*usageNode]{}

	// usageResult is the finished scan the dialogs read, swapped in
	// atomically; the last scan wins.
	usageResult atomic.Pointer[usageScan]

	// usageRunner serializes the treemap scans — one at a time, the
	// newest request next — repainting every usageTick so the status
	// line's live folder count moves.
	usageRunner = &taskRunner[usageRequest]{
		run:  runUsageScan,
		tick: 400 * time.Millisecond,
	}

	// usageVisited is the folders the running scan has visited, for the
	// status line's live count.
	usageVisited atomic.Int64
)

// registerUsagePlugin registers the 可视化查看占用 plugin: both context
// menus' treemap item — the current folder on the pane background, the
// selected folder on a row.
func registerUsagePlugin() {
	RegisterPlugin(Plugin{
		ID:   usagePluginID,
		Name: "可视化查看占用",
		Desc: "右键用矩形树图查看文件夹占用情况",
		MenuItems: func(a *App, sel []Entry) []PluginMenuItem {
			if len(sel) == 0 {
				return []PluginMenuItem{{
					Label:    "可视化查看占用",
					Disabled: specialPath(a.cur().path),
					Run: func() {
						a.openUsage(a.cur().path)
					},
				}}
			}
			if len(sel) == 1 && sel[0].IsDir && !specialPath(sel[0].Path) {
				return []PluginMenuItem{{
					Label: "可视化查看占用",
					Run: func() {
						a.openUsage(sel[0].Path)
					},
				}}
			}
			return nil
		},
	})
}

// usageRequest is one scan the scheduler owes: the folder and whether the
// user shows hidden items at the time it was asked for.
type usageRequest struct {
	root   string
	hidden bool
}

// usageNode is one folder or file in the scan's tree. A file's Size and
// Files are its own; a folder's are filled in bottom-up after the walk.
type usageNode struct {
	Name     string
	Path     string
	IsDir    bool
	Ext      string
	Size     int64
	Files    int
	Children []*usageNode
}

// usageScan is one finished scan: the tree and what it covered.
type usageScan struct {
	root      string
	tree      *usageNode
	files     int
	dirs      int
	truncated bool
	err       string
}

// usageOpenFlag returns this window's dialog flag, allocating it on first
// use (plugintask.go's per-window appFlag).
func (a *App) usageOpenFlag() *bool {
	return usageDlg.get(a)
}

// openUsage opens the dialog and kicks a scan of the folder.
func (a *App) openUsage(path string) {
	*a.usageOpenFlag() = true
	a.usageSetHere(nil)
	a.usageSetPick(nil)
	a.requestUsage(path)
}

// requestUsage starts the subtree scan in the background, unless the page
// is a special one or the plugin is off. The taskRunner serializes the
// work — one scan at a time, a request that arrives mid-scan runs right
// after it — and repaints on its tick.
func (a *App) requestUsage(root string) {
	if !a.pluginEnabled(usagePluginID) {
		return
	}
	if root == "" || specialPath(root) {
		return
	}
	usageRunner.submit(usageRequest{root: root, hidden: a.showHidden})
}

// runUsageScan is the taskRunner's task: one scan, its result swapped in
// atomically and the window repainted. The runner owns serialization and
// the progress repaint, so this is just the scan plus its publish.
func runUsageScan(req usageRequest) {
	usageVisited.Store(0)
	res := scanUsage(req.root, req.hidden)
	usageResult.Store(res)
	repaint()
}

// usageRunningNow is whether a scan is running, for the status line.
func usageRunningNow() bool {
	return usageRunner.busy()
}

// scanUsage walks root's subtree and totals every folder's bytes. It is
// the background half: bounded by a deadline and a node cap, so a huge
// tree cannot wedge the app. Six walkers read folders at once; the tree
// assembles level by level, so totals fill in one bottom-up pass over
// the folders in discovery order.
func scanUsage(root string, hidden bool) *usageScan {
	res := &usageScan{root: root}
	rootNode := &usageNode{Name: filepath.Base(root), Path: root, IsDir: true}
	res.tree = rootNode
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		res.err = "无法读取该文件夹"
		return res
	}
	deadline := time.Now().Add(usageTimeout)
	truncated := atomic.Bool{}
	all := []*usageNode{rootNode}
	frontier := []*usageNode{rootNode}
	var seen atomic.Int64
	for len(frontier) > 0 {
		if truncated.Load() || time.Now().After(deadline) || seen.Load() >= usageMaxNodes {
			truncated.Store(true)
			break
		}
		next := make([]*usageNode, 0, len(frontier))
		var mu sync.Mutex
		var idx atomic.Int64
		wg := new(sync.WaitGroup)
		for w := 0; w < usageWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(idx.Add(1) - 1)
					if i >= len(frontier) {
						return
					}
					if truncated.Load() || time.Now().After(deadline) {
						truncated.Store(true)
						return
					}
					n := frontier[i]
					usageVisited.Add(1)
					entries, err := os.ReadDir(n.Path)
					if err != nil {
						continue // unreadable folder: skip it, keep walking
					}
					for _, e := range entries {
						name := e.Name()
						if !hidden && strings.HasPrefix(name, ".") {
							continue
						}
						if e.Type()&os.ModeSymlink != 0 || !e.Type().IsRegular() && !e.IsDir() {
							continue
						}
						if seen.Add(1) > usageMaxNodes {
							truncated.Store(true)
							continue
						}
						child := &usageNode{
							Name:  name,
							Path:  filepath.Join(n.Path, name),
							IsDir: e.IsDir(),
						}
						if !child.IsDir {
							info, err := e.Info()
							if err != nil {
								continue
							}
							child.Size = info.Size()
							child.Files = 1
							child.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
						}
						mu.Lock()
						n.Children = append(n.Children, child)
						if child.IsDir {
							next = append(next, child)
						}
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		all = append(all, next...)
		frontier = next
	}
	// Discovery order is breadth first, so every folder sits in all
	// before its subfolders: walking it backwards totals each one from
	// children that are already totalled.
	for i := len(all) - 1; i >= 0; i-- {
		n := all[i]
		for _, ch := range n.Children {
			n.Size += ch.Size
			n.Files += ch.Files
		}
	}
	res.files = rootNode.Files
	res.dirs = len(all)
	res.truncated = truncated.Load()
	return res
}

// usageSetHere sets this window's drill position, the node the treemap
// shows; nil means the scan's root (plugintask.go's per-window appVal).
func (a *App) usageSetHere(n *usageNode) {
	usageDrillAt.set(a, n)
}

// usageHereNode is this window's drill position, or nil.
func (a *App) usageHereNode() *usageNode {
	return usageDrillAt.get(a)
}

// usageSetPick sets this window's picked cell, the one the info line
// shows; nil clears it.
func (a *App) usageSetPick(n *usageNode) {
	usagePickAt.set(a, n)
}

// usagePickedNode is this window's picked cell, or nil.
func (a *App) usagePickedNode() *usageNode {
	return usagePickAt.get(a)
}

// usageFind is the node at path in the tree, or nil: the drill controls
// look their targets up here.
func usageFind(n *usageNode, path string) *usageNode {
	if n == nil || path == "" {
		return nil
	}
	if n.Path == path {
		return n
	}
	for _, ch := range n.Children {
		if ch.Path == path {
			return ch
		}
		if ch.IsDir && strings.HasPrefix(path, ch.Path+"/") {
			if f := usageFind(ch, path); f != nil {
				return f
			}
		}
	}
	return nil
}

// usageParent is this window's drill position's parent in the tree, or
// nil when the position is the root it disables 上一级 at.
func (a *App) usageParent() *usageNode {
	here := a.usageHereNode()
	res := usageResult.Load()
	if here == nil || res == nil || here.Path == res.root {
		return nil
	}
	return usageFind(res.tree, filepath.Dir(here.Path))
}

// sortedUsage sorts children largest first, ties keeping folder order.
func sortedUsage(children []*usageNode) []*usageNode {
	out := make([]*usageNode, len(children))
	copy(out, children)
	slices.SortStableFunc(out, func(a, b *usageNode) int { return cmp.Compare(b.Size, a.Size) })
	return out
}

// usageCell is one treemap rectangle: the node and where it sits in the
// map's own coordinates.
type usageCell struct {
	node       *usageNode
	x, y, w, h float32
}

// usageCells picks the children to draw and lays them into r: the largest
// children individually, everything under the share floor — and past the
// cell cap — merged into one 其他 cell, then the squarified treemap.
func usageCells(children []*usageNode, r ui.Rect) []usageCell {
	var total int64
	for _, n := range children {
		total += n.Size
	}
	if total <= 0 {
		return nil
	}
	sorted := sortedUsage(children)
	var items []*usageNode
	for _, n := range sorted {
		if len(items) >= usageMaxCells-1 || float64(n.Size)/float64(total) < usageMinShare {
			break
		}
		items = append(items, n)
	}
	if len(items) == 0 {
		// Nothing clears the floor: show the largest cells the cap allows.
		for _, n := range sorted {
			if len(items) >= usageMaxCells-1 || n.Size <= 0 {
				break
			}
			items = append(items, n)
		}
	}
	var rest int64
	for _, n := range sorted[len(items):] {
		rest += n.Size
	}
	if rest > 0 {
		items = append(items, &usageNode{Name: "其他", Size: rest})
	}
	return squarify(items, r)
}

// squarify lays items into r by the squarified treemap algorithm: items
// must be sorted descending by size, the cells come out area-proportional
// and as square as the split allows.
func squarify(items []*usageNode, r ui.Rect) []usageCell {
	cells := make([]usageCell, 0, len(items))
	remaining := 0.0
	for _, n := range items {
		remaining += float64(n.Size)
	}
	if remaining <= 0 || r.W <= 0 || r.H <= 0 {
		return cells
	}
	area := float64(r.W) * float64(r.H)
	x, y, w, h := r.X, r.Y, r.W, r.H
	i := 0
	for i < len(items) {
		vertical := w >= h
		rowSum := 0.0
		best := math.MaxFloat64
		j := i
		for j < len(items) {
			sum := rowSum + float64(items[j].Size)
			worst := rowWorst(items, i, j+1, sum, remaining, float64(w), float64(h))
			if j > i && worst > best {
				break
			}
			best = worst
			rowSum = sum
			j++
		}
		if j == i {
			j = i + 1
			rowSum = float64(items[i].Size)
		}
		strip := rowSum / remaining * area
		if vertical {
			t := float32(strip / float64(h))
			yy := y
			for k := i; k < j; k++ {
				ch := float32(float64(items[k].Size) / rowSum * float64(h))
				cells = append(cells, usageCell{items[k], x, yy, t, ch})
				yy += ch
			}
			x += t
			w -= t
		} else {
			t := float32(strip / float64(w))
			xx := x
			for k := i; k < j; k++ {
				cw := float32(float64(items[k].Size) / rowSum * float64(w))
				cells = append(cells, usageCell{items[k], xx, y, cw, t})
				xx += cw
			}
			y += t
			h -= t
		}
		remaining -= rowSum
		area -= strip
		i = j
	}
	return cells
}

// rowWorst is the row's worst cell aspect: each cell runs along the strip's
// long side by its share of the row, at the strip's thickness.
func rowWorst(items []*usageNode, lo, hi int, rowSum, remaining, w, h float64) float64 {
	if rowSum <= 0 || remaining <= 0 || w <= 0 || h <= 0 {
		return math.MaxFloat64
	}
	worst := 0.0
	for k := lo; k < hi; k++ {
		a := float64(items[k].Size)
		if a <= 0 {
			return math.MaxFloat64
		}
		along, thick := 0.0, 0.0
		if w >= h {
			along = a / rowSum * h
			thick = rowSum / remaining * w
		} else {
			along = a / rowSum * w
			thick = rowSum / remaining * h
		}
		ar := along / thick
		if thick > along {
			ar = thick / along
		}
		if ar > worst {
			worst = ar
		}
	}
	return worst
}

// — The dialog —

// usageModal shows the dialog while it is open. Called from filePane, the
// per-frame hook this file has without touching main.go.
