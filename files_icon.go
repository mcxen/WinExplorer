package main

// files_icon.go draws the icon views (small to extra large tiles): the grid
// measurement, tile sizes and fonts, thumbnails, folder covers and video
// badges. It was split out of files.go with the other per-view code.

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// iconView lays tiles in a grid. Extra large, large and medium put the
// label under the icon; small icons put it beside, in a shorter tile.
func (a *App) iconView(c *ui.Context, entries []Entry) {
	// reload records the mount kind outside view construction. The view only
	// reads that cached decision: probing statfs or mount tables here would
	// block the UI whenever the cache expires.
	box := a.iconBoxSize(a.mode)
	minW, height := a.iconTileSize(a.mode, box)
	if a.grouping != groupNone && !specialPath(a.cur().path) {
		scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(iconGridPad)
		scroll.Children(func() {
			rows := a.groupedRows(entries)
			var group []Entry
			warm := 0 // tiles decoded so far across the groups
			emit := func() {
				if len(group) == 0 {
					return
				}
				// The folder's first tileWarmup tiles decode even when groups
				// split the listing: each group's tiles count on from the
				// last, rather than warming up again from zero.
				base := warm
				warm += len(group)
				grid := ui.GridView(c, nil, len(group), minW, height, func(i int) {
					if i >= 0 && i < len(group) {
						a.iconTile(c, group[i], box, base+i)
					}
				}).FillWidth().Shrink(0)
				grid.Children(func() {})
				group = group[:0]
			}
			for _, r := range rows {
				if r.header != "" {
					emit()
					a.groupHeaderRow(c, r.header, r.count)
					continue
				}
				group = append(group, r.e)
			}
			emit()
			if len(rows) == 0 {
				a.paneMessage(c, "此文件夹为空")
			}
		})
		// The per-group grids share the scroll's width, so its box says how
		// many tiles a row takes.
		a.measureGrid(c, scroll, minW, height, iconGridPad)
		a.bindPaneMenu(c, scroll)
		// The grouped icon view scrolls as one column of grids, which has no
		// row index to reveal. Drop the request so a later switch of view does
		// not jump to it.
		a.cur().reveal = ""
		return
	}
	t := a.cur()
	t.grid.Key = func(i int) any {
		if i < 0 || i >= len(entries) || entries[i].Path == "" {
			return i
		}
		return entries[i].Path
	}
	if i := entryIndexOf(entries, t.reveal); i >= 0 {
		t.grid.ScrollIntoView(i)
		t.reveal = ""
	} else if t.reveal != "" {
		t.reveal = ""
	}
	grid := ui.GridView(c, &t.grid, len(entries), minW, height, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.iconTile(c, entries[i], box, i)
	}).Grow(1).FillWidth().MinHeight(0).Padding(iconGridPad)
	grid.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, grid)
	a.measureGrid(c, grid, minW, height, iconGridPad)
}

// iconGridPad is the padding the icon views leave around their grid, which
// the columns and the grid's measurement both take off its width.
const iconGridPad float32 = 8

// measureGrid records the icon view's shape as the frame before laid it
// out: how many tiles a row takes, and how many rows the pane shows, for
// the arrows' row-sized steps and the Page keys. box is the element the
// tiles are laid in — the grid, or the scroll holding the grouped view's
// per-group grids — and pad the padding between its edge and them.
func (a *App) measureGrid(c *ui.Context, box ui.Element, minW, height, pad float32) {
	b := box.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	t := a.cur()
	t.gridCols = gridCols(c, b.W, minW, pad)
	t.gridRows = max(int((b.H-pad*2)/height), 1)
}

// gridCols is how many items a grid lays in a row of width w, as GridView
// counts them: what fits at least minW wide and gap apart, inside pad on
// either side. A keyboard step down lands on the item under this one only
// where the count agrees with the grid's own.
func gridCols(c *ui.Context, w, minW, pad float32) int {
	gap := c.Theme().Space(2)
	return max(int((w-pad*2+gap)/(minW+gap)), 1)
}

// iconBaseSize is a view's icon box before 查看>放大/缩小: the four sizes
// Explorer's icon views use.
func iconBaseSize(mode ViewMode) float32 {
	switch mode {
	case ViewExtraLarge:
		return 96
	case ViewLargeIcons:
		return 72
	case ViewMediumIcons:
		return 48
	default:
		return 32
	}
}

// isIconMode reports whether mode lays out a grid of tiles, the views
// 放大/缩小 resizes.
func isIconMode(mode ViewMode) bool {
	switch mode {
	case ViewSmallIcons, ViewMediumIcons, ViewLargeIcons, ViewExtraLarge:
		return true
	}
	return false
}

// iconBoxSize is a view's icon box with the 放大/缩小 factor applied, so
// 超大图标 reaches as far as 4× its 96 px.
func (a *App) iconBoxSize(mode ViewMode) float32 {
	base := iconBaseSize(mode)
	if !isIconMode(mode) {
		return base
	}
	return base * a.iconZoom
}

func (a *App) iconTileSize(mode ViewMode, box float32) (minW, height float32) {
	if mode == ViewSmallIcons {
		return box + 148, max(44, box+12)
	}
	h := box + 52
	if a.cur().path == pathThisPC {
		h += 12 // room for the drive usage bar under the name
	}
	return box + 40, h
}

// tileFont is the tile label's size, which grows a little with the tile so
// a 384 px icon does not sit over a 12 pt name.
func tileFont(box float32) float32 {
	f := 12 + (box-96)/32
	return min(max(f, 12), 18)
}

// tileWarmup is how many tiles of a network folder decode their pictures
// before anyone asks: the first rows fill in as on a local folder, and the
// rest wait for the pointer or the selection. The cap is what keeps opening
// a share from downloading the whole folder's media.
const tileWarmup = 12

// iconTile is one grid cell. ButtonBase is a row, so a small icon sits
// beside its name. Larger sizes put a column in the button: the icon, then
// the name under it. i is the tile's place in the listing, which decides
// whether a network tile warms up its picture or waits to be looked at.
func (a *App) iconTile(c *ui.Context, e Entry, box float32, i int) {
	tile := ui.ButtonBase(c.Key(e.Path)).Fill().Radius(4).Label(e.Name)
	if e.VolTotal > 0 {
		tile.Tooltip(driveCapacity(e) + "（" + drivePercent(e) + " 可用）")
	}
	if a.mode == ViewSmallIcons {
		tile.Padding(4, 6).Gap(8).AlignItems(ui.Center).Justify(ui.Start)
	}
	a.paintSelect(tile, e.Path)
	look := aiFxRow(c, tile, e.Path, 0, 4)
	// A network tile decodes its picture for the first rows of the folder and
	// for a tile under the pointer or in the selection; the rest keep the
	// drawn glyph until someone looks, so opening a share does not download
	// the whole folder's media at once.
	eager := !a.cur().networkDir || (a.cur().networkWarm && i < tileWarmup) || tile.Hovered() || a.isSelected(e.Path)
	tile.Children(func() {
		if a.mode == ViewSmallIcons {
			a.nameCell(c, e, box, 12)
			return
		}
		font := tileFont(box)
		body := ui.Column(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(6, 4)
		body.Children(func() {
			glyph := ui.Box(c).Size(box, box).Shrink(0)
			glyph.Children(func() {
				a.tileGlyph(c, e, box, eager)
			})
			if a.cur().renaming == e.Name {
				a.renameField(c, font)
				return
			}
			name := a.displayName(e)
			if strings.TrimSpace(a.cur().query) != "" {
				name = e.Name
			}
			label := searchNameText(c, a.activeSearchQuery(), tileName(name, tileNameRunes(box)), font).
				TextAlign(ui.Center).MaxLines(2).FillWidth()
			if look.flash > 0 {
				label.TextColor(flashColor(colorText(), look))
			}
			if e.VolTotal > 0 {
				bar := ui.Box(c).Width(box).Height(6).Shrink(0)
				bar.Draw(func(p *ui.Painter, r ui.Rect) {
					drawCapacityBar(p, r, e.VolFree, e.VolTotal)
				})
			}
		})
	})
	a.bindItemInput(c, tile, e)
	a.attachRowMenu(c, tile, e)
	// The grouped icon views lay every tile out inside one plain Scroll, so
	// the tile the keyboard moved to is built even off screen and raises
	// itself: the ungrouped grid scrolls to its row through GridState.
	if a.grouping != groupNone && !specialPath(a.cur().path) && a.cur().reveal == e.Path {
		tile.ScrollIntoView()
	}
}

// tileGlyph is a tile's icon area: a picture or video shows its own frame,
// a PDF shows its first page when a renderer is available, an HTML file
// shows its favicon when one is local, a folder holding media keeps its
// folder shape with the pictures inside it, and everything else is the
// drawn file or folder glyph. The drawn glyphs stay under thumbMinBox,
// where 16 px of picture says less than the file-type colour. eager false
// leaves the picture undecoded — the network tile's rule — and the drawn
// glyph stands in until the tile is looked at.
func (a *App) tileGlyph(c *ui.Context, e Entry, box float32, eager bool) {
	if box >= thumbMinBox && eager {
		if e.IsDir {
			if a.folderCoverEligible(e) {
				if bm := folderCoverThumb(e.Path, box); bm != nil {
					a.folderCoverTile(c, box, bm)
					return
				}
			}
		} else if bm := mediaThumb(e.Path, box); bm != nil {
			a.mediaTile(c, e, box, bm)
			return
		}
	}
	a.fileIcon(c, e, box)
}

// mediaTile is a picture's tile: the picture fills it, and a video's gets
// the play badge over it.
func (a *App) mediaTile(c *ui.Context, e Entry, box float32, bm *ui.Bitmap) {
	frame := ui.Box(c).Size(box, box).Shrink(0).Radius(4).Clip()
	frame.Children(func() {
		ui.Image(c, bm).Fit(ui.Cover).Size(box, box)
		if isVideoExt(lowerExt(e.Path)) {
			a.videoBadge(c, box)
		}
	})
}

// folderCoverTile is a media folder's tile. The folder is still a folder —
// its tab, its edges, its colour — and the pictures of its media fill the
// front panel inside it, which is how Explorer draws a folder that holds
// pictures: the folder's silhouette is what tells it apart from a file
// among the tiles.
func (a *App) folderCoverTile(c *ui.Context, box float32, bm *ui.Bitmap) {
	glyph := ui.Box(c).Size(box, box).Shrink(0)
	glyph.Children(func() {
		shell := ui.Box(c).Absolute().Left(0).Top(0).Size(box, box).Shrink(0)
		shell.Draw(func(p *ui.Painter, r ui.Rect) {
			drawFolder(p, r, colorFolder())
		})
		panel := folderCoverPanel(box)
		cover := ui.Box(c).Absolute().Left(panel.X).Top(panel.Y).
			Size(panel.W, panel.H).Shrink(0).
			Radius(folderCoverPanelRadius(box)).Clip()
		cover.Children(func() {
			ui.Image(c, bm).Fit(ui.Cover).Size(panel.W, panel.H)
		})
	})
}

// videoBadge marks a video's thumbnail with the round play badge Explorer
// paints over one.
func (a *App) videoBadge(c *ui.Context, box float32) {
	size := min(max(box*0.2, 20), 40)
	ui.Box(c).Absolute().Left(6).Bottom(6).Size(size, size).Shrink(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			d := min(r.W, r.H)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: d, H: d}, ui.RGBA(0, 0, 0, 0.55), d/2)
			var tri ui.Path
			tri.MoveTo(r.X+d*0.36, r.Y+d*0.28)
			tri.LineTo(r.X+d*0.74, r.Y+d*0.50)
			tri.LineTo(r.X+d*0.36, r.Y+d*0.72)
			tri.Close()
			p.FillPath(&tri, ui.RGB(255, 255, 255))
		})
}

// folderCoverEligible reports whether a folder tile may show a cover of
// its contents: only real folders may, and never a volume root, whose
// contents are not a folder's.
func (a *App) folderCoverEligible(e Entry) bool {
	if e.Path == "" || specialPath(e.Path) || isVolumeRoot(e.Path) {
		return false
	}
	return a.cur().path != pathThisPC
}

// isVolumeRoot reports whether path is a mount point — "/" or
// "/Volumes/…" — rather than a folder inside one.
func isVolumeRoot(path string) bool {
	clean := filepath.Clean(path)
	if clean == "/" || clean == "." {
		return true
	}
	if vol := filepath.VolumeName(clean); vol != "" && filepath.Clean(vol) == clean {
		return true
	}
	return filepath.Dir(clean) == clean
}
