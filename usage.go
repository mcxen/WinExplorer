package main

import (
	"cmp"
	"fmt"
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

// usage.go is the 可视化查看占用 plugin: the folder context menus gain an
// item that scans the folder's subtree and opens a dialog with a
// squarified treemap — WizTree's rectangle map — plus a largest-first
// list. A rectangle's area is its share of the folder; clicking picks it,
// double-clicking a folder drills in, and the list rows behave the same.
//
// The dialog flag, the drill position and the picked cell are per window
// (keyed by App, as dups.go's flag is); the scan result is process-wide,
// so the dialog shows the folder the last scan ran for. One scan runs at
// a time; a request that arrives mid-scan runs right after it.

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
func (a *App) usageModal(c *ui.Context) {
	flag := a.usageOpenFlag()
	if !*flag {
		return
	}
	floatDialog(c, flag, "可视化查看占用", 560, false, func() {
		a.usageBody(c, flag)
	})
}

// usageBody is the dialog: the folder's status, the drill position row,
// the treemap, the picked cell's line, the largest-first list and the
// buttons.
func (a *App) usageBody(c *ui.Context, flag *bool) {
	res := usageResult.Load()
	here := a.usageHereNode()
	if here == nil && res != nil {
		here = res.tree
		a.usageSetHere(here)
	}
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		ui.Text(c, "可视化查看占用").FontSize(14).FontWeight(600).TextColor(colorText())
		a.usageStatus(c, res)
		a.usagePlaceRow(c, here, flag)
		a.usageMap(c, res, here)
		a.usagePickedInfo(c, here)
		a.usageTopList(c, here)
		a.usageButtons(c, res, flag)
	})
}

// usageStatus is the muted line under the title: what the scan is doing
// with its live folder count, and what it covered.
func (a *App) usageStatus(c *ui.Context, res *usageScan) {
	line := "正在扫描…"
	switch {
	case usageRunningNow():
		line = fmt.Sprintf("正在扫描…（已访问 %d 个文件夹）", usageVisited.Load())
	case res == nil:
	case res.err != "":
		line = res.err
	case len(res.tree.Children) == 0:
		line = "这个文件夹是空的"
	default:
		line = fmt.Sprintf("共 %s · %d 个文件 · %d 个文件夹",
			formatBytes(res.tree.Size), res.files, res.dirs)
		if res.truncated {
			line += "（已达上限，结果可能不全）"
		}
	}
	ui.Text(c, line).FontSize(12).TextColor(colorMuted()).SingleLine().FillWidth()
}

// usagePlaceRow is the drill position's path with 上一级 and 在窗口中打开.
func (a *App) usagePlaceRow(c *ui.Context, here *usageNode, flag *bool) {
	if here == nil {
		return
	}
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, here.Path).FontSize(11).TextColor(colorMuted()).
			SingleLine().Grow(1).MinWidth(0)
		if a.usageParent() != nil {
			if usageButton(c, "上一级", false) {
				a.usageSetHere(a.usageParent())
				a.usageSetPick(nil)
			}
		}
		if usageButton(c, "在窗口中打开", false) {
			*flag = false
			a.navigate(here.Path)
		}
	})
}

// usageMap is the treemap: cells squarified over the canvas, one
// rectangle per child of the drill position.
func (a *App) usageMap(c *ui.Context, res *usageScan, here *usageNode) {
	shell := ui.Box(c).FillWidth().Height(280).Shrink(0).Radius(innerRadius()).
		Border(1, colorDivider()).Background(colorContent()).Clip()
	shell.Children(func() {
		if res == nil || here == nil {
			ui.Text(c, "正在扫描…").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		if len(here.Children) == 0 {
			ui.Text(c, "这个文件夹是空的").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		if here.Size <= 0 {
			ui.Text(c, "没有可显示的占用").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		b := shell.Bounds()
		if b.W <= 0 {
			return
		}
		r := ui.Rect{X: 2, Y: 2, W: b.W - 4, H: b.H - 4}
		for _, cell := range usageCells(here.Children, r) {
			a.usageCellView(c, cell, here)
		}
	})
}

// usageCellView is one treemap rectangle: the pastel fill, the label when
// the cell has room, click to pick, double-click to drill in.
func (a *App) usageCellView(c *ui.Context, cell usageCell, parent *usageNode) {
	if cell.w < 3 || cell.h < 3 {
		return
	}
	n := cell.node
	col := usageColor(n)
	box := ui.Box(c.Key(n.Path)).Absolute().
		Left(cell.x+1).Top(cell.y+1).
		Size(max(cell.w-2, 1), max(cell.h-2, 1)).Radius(2).Clip()
	switch {
	case a.usagePickedNode() == n:
		box.Background(col.Mix(ui.RGB(0, 0, 0), 0.18)).Border(1.5, colorAccent())
	case box.Hovered():
		box.Background(col.Mix(ui.RGB(255, 255, 255), 0.3))
	default:
		box.Background(col)
	}
	box.Children(func() {
		a.usageCellLabel(c, cell, n, parent)
	})
	if box.Clicked() {
		a.usageSetPick(n)
	}
	if box.DoubleClicked() && n.IsDir {
		a.usageSetHere(n)
		a.usageSetPick(nil)
	}
}

// usageCellLabel overlays the name and size when the cell has room.
func (a *App) usageCellLabel(c *ui.Context, cell usageCell, n, parent *usageNode) {
	if cell.w < 44 || cell.h < 16 {
		return
	}
	ui.Column(c).Fill().Padding(2, 4).Gap(1).AlignItems(ui.Start).Children(func() {
		ui.Text(c, n.Name).FontSize(11).TextColor(usageInk(n)).MaxLines(1).FillWidth()
		if cell.w >= 64 && cell.h >= 30 {
			line := formatBytes(n.Size)
			if share := usageShare(n, parent); share > 0 {
				line += fmt.Sprintf(" · %.1f%%", share*100)
			}
			ui.Text(c, line).FontSize(10).TextColor(usageInk(n)).MaxLines(1).FillWidth()
		}
	})
}

// usagePickedInfo is the picked cell's line: its name, size and share of
// the drilled folder.
func (a *App) usagePickedInfo(c *ui.Context, here *usageNode) {
	n := a.usagePickedNode()
	if n == nil {
		return
	}
	line := fmt.Sprintf("选中：%s · %s", n.Name, formatBytes(n.Size))
	if share := usageShare(n, here); share > 0 {
		line += fmt.Sprintf(" · 占 %s 的 %.1f%%", here.Name, share*100)
	}
	ui.Text(c, line).FontSize(12).TextColor(colorText()).SingleLine().FillWidth()
}

// usageTopList is the largest-first list of the drill position's children:
// the rows click and double-click like the map's cells.
func (a *App) usageTopList(c *ui.Context, here *usageNode) {
	if here == nil || len(here.Children) == 0 {
		return
	}
	top := sortedUsage(here.Children)
	if len(top) > 30 {
		top = top[:30]
	}
	list := ui.Scroll(c).FillWidth().MaxHeight(180).Gap(2)
	list.Children(func() {
		for _, n := range top {
			a.usageTopRow(c, n, here)
		}
	})
}

// usageTopRow is one list row: the colour chip, the name, the share, the
// size and the share bar.
func (a *App) usageTopRow(c *ui.Context, n, here *usageNode) {
	row := ui.Row(c.Key(n.Path)).FillWidth().Height(24).Shrink(0).
		Padding(0, 8).AlignItems(ui.Center).Gap(8).Radius(rowR())
	switch {
	case a.usagePickedNode() == n:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		chip := ui.Box(c).Size(8, 8).Shrink(0).Radius(2)
		chip.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, usageColor(n), 2)
		})
		name := n.Name
		if n.IsDir {
			name += "/"
		}
		ui.Text(c, name).FontSize(12).TextColor(colorText()).
			Grow(1).MinWidth(0).MaxLines(1)
		ui.Text(c, fmt.Sprintf("%.1f%%", usageShare(n, here)*100)).
			FontSize(11).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, formatBytes(n.Size)).FontSize(11).TextColor(colorMuted()).Shrink(0)
		bar := ui.Box(c).Width(90).Height(6).Shrink(0).Radius(3)
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, colorDivider(), 3)
			if w := r.W * float32(usageShare(n, here)); w > 0 {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: min(w, r.W), H: r.H}, usageColor(n), 3)
			}
		})
	})
	if row.Clicked() {
		a.usageSetPick(n)
	}
	if row.DoubleClicked() && n.IsDir {
		a.usageSetHere(n)
		a.usageSetPick(nil)
	}
}

// usageButtons is the dialog's button row: 重新扫描 and 关闭.
func (a *App) usageButtons(c *ui.Context, res *usageScan, flag *bool) {
	btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
	btns.Children(func() {
		if res != nil && res.err == "" {
			if usageButton(c, "重新扫描", false) {
				a.requestUsage(res.root)
			}
		}
		if usageButton(c, "关闭", true) {
			*flag = false
		}
	})
}

// usageButton is a dialog button in the primary or secondary style,
// reporting whether it was clicked. Height 28, one under uikitSecondaryButton,
// for the space-tight scan dialog — with that button's padding and floor width,
// so 重新扫描 and 关闭 come out even.
func usageButton(c *ui.Context, text string, primary bool) bool {
	btn := ui.ButtonBase(c).Height(28).Padding(0, btnPadX).MinWidth(btnMinW).Shrink(0).Radius(rowR()).Label(text)
	if primary {
		btn = btn.Background(colorAccent())
	} else {
		btn = btn.Background(colorAddress()).Border(1, colorDivider())
	}
	btn.Children(func() {
		fg := colorText()
		if primary {
			fg = colorBtnText()
		}
		ui.Text(c, text).FontSize(12).TextColor(fg)
	})
	return btn.Clicked()
}

// — Colours —

// usageColor is a cell's pastel fill: folders one blue, files tinted by
// their extension's hue, 其他 gray — light enough for dark text.
func usageColor(n *usageNode) ui.Color {
	if n.IsDir && n.Path != "" {
		return hslColor(211, 0.58, 0.76)
	}
	if !n.IsDir && n.Ext != "" {
		return hslColor(float64(extHue(n.Ext)), 0.58, 0.76)
	}
	return hslColor(0, 0, 0.82)
}

// usageInk is the label ink on a cell's pastel fill: dark enough to read
// on every tint the map uses.
func usageInk(*usageNode) ui.Color {
	return ui.RGB(28, 32, 44)
}

// extHue spreads extensions around the wheel by their bytes, so the same
// type keeps its colour and different types spread apart.
func extHue(ext string) int {
	h := 0
	for i := 0; i < len(ext); i++ {
		h = h*31 + int(ext[i])
	}
	if h < 0 {
		h = -h
	}
	return h % 360
}

// hslColor converts HSL — h in degrees, s and l from 0 to 1 — to the UI's
// color.
func hslColor(h, s, l float64) ui.Color {
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(math.Mod(h, 360)+360, 360) / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	r, g, b := 0.0, 0.0, 0.0
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return ui.RGB(uint8((r+m)*255+0.5), uint8((g+m)*255+0.5), uint8((b+m)*255+0.5))
}

// usageShare is the node's share of its parent, 0 when the parent has no
// bytes to divide.
func usageShare(n, parent *usageNode) float64 {
	if parent == nil || parent.Size <= 0 {
		return 0
	}
	return float64(n.Size) / float64(parent.Size)
}
