package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistant_openui_more.go paints the OPENUI-MORE components: Files rows,
// the Steps timeline, StatCards tiles and the Gallery grid. Thumbnails,
// icons and the quick preview are the ones MediaList uses.

const (
	openFileIconBox  float32 = 36
	openGalleryTileW float32 = 120
	openGalleryTileH float32 = 90
	openStatTileW    float32 = 150
	openGalleryGap   float32 = 8
	// openGalleryMinW is the narrowest tile a fixed column count may give;
	// below it the grid falls back to auto fit.
	openGalleryMinW float32 = 72
	// openStatTTL is how long a Files row trusts its cached stat.
	openStatTTL = 2 * time.Second
	// openClickDelay holds a Gallery tile's quick preview until a second
	// click could no longer make it a double click.
	openClickDelay = 300 * time.Millisecond
)

// Hooks the tests swap: the system opener, the clock and navigation.
var (
	openUISysOpen = openWithOS
	openUINow     = time.Now
)

type openStatEntry struct {
	info os.FileInfo
	err  error
	at   time.Time
}

// openStatCache keeps Files rows from statting every frame.
var openStatCache struct {
	sync.Mutex
	m map[string]openStatEntry
}

func openStat(path string) (os.FileInfo, error) {
	now := openUINow()
	openStatCache.Lock()
	defer openStatCache.Unlock()
	if e, ok := openStatCache.m[path]; ok && now.Sub(e.at) < openStatTTL && !e.at.After(now) {
		return e.info, e.err
	}
	if openStatCache.m == nil || len(openStatCache.m) > 512 {
		openStatCache.m = map[string]openStatEntry{}
	}
	info, err := os.Stat(path)
	openStatCache.m[path] = openStatEntry{info, err, now}
	return info, err
}

// openSystemOpen opens local in the system app, recording it as recent.
func (a *App) openSystemOpen(local, name string) {
	if err := openUISysOpen(local); err != nil {
		a.statusMsg = "无法打开：" + name
		return
	}
	a.recordRecent(local)
}

func (a *App) paintOpenMore(c *ui.Context, v *ouiView) {
	switch v.Kind {
	case "files":
		a.paintOpenFiles(c, v)
	case "steps":
		a.paintOpenSteps(c, v)
	case "stats":
		a.paintOpenStats(c, v)
	case "gallery":
		a.paintOpenGallery(c, v)
	}
}

func openMoreTitle(c *ui.Context, title string) {
	if title != "" {
		ui.Text(c.Key("title"), title).FontSize(14).FontWeight(600).TextColor(colorText()).
			Wrap().Selectable().Padding(0, 0, 4, 0)
	}
}

// openFileMeta fills size and time from the disk when the program left
// them out; a folder or a missing file keeps what was written.
func openFileMeta(it *ouiView) (name, size, when string, dir bool) {
	name, size, when = it.Text, it.Label, it.Placeholder
	local := openMediaLocal(it.Path)
	if name == "" {
		name = baseName(openMediaURLPath(it.Path))
	}
	if local != "" && filepath.IsAbs(local) {
		if st, err := openStat(local); err == nil {
			dir = st.IsDir()
			if size == "" && !dir {
				size = formatBytes(st.Size())
			}
			if when == "" {
				when = st.ModTime().Format("2006-01-02 15:04")
			}
		}
	}
	return
}

func (a *App) paintOpenFiles(c *ui.Context, v *ouiView) {
	ui.Column(c).FillWidth().MinWidth(0).Label("OpenUI files").Children(func() {
		openMoreTitle(c, v.Text)
		for i, it := range v.Items {
			if i > 0 {
				ui.Box(c.Key("div" + itoa(i))).FillWidth().Height(1).Background(colorDivider())
			}
			a.paintOpenFileRow(c.Key("f"+itoa(i)), it)
		}
	})
}

func (a *App) paintOpenFileRow(c *ui.Context, it *ouiView) {
	name, size, when, dir := openFileMeta(it)
	local := openMediaLocal(it.Path)
	row := ui.ButtonBase(c).FillWidth().MinWidth(0).Radius(6).Padding(6, 6).Label("打开 " + name)
	row.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Center).Children(func() {
			verb := "预览 "
			if dir {
				verb = "进入 "
			}
			icon := ui.ButtonBase(c.Key("icon")).Size(openFileIconBox, openFileIconBox).Shrink(0).Radius(6).Clip().
				Label(verb + name)
			icon.Children(func() {
				var bm *ui.Bitmap
				if local != "" && !dir && (isImageExt(lowerExt(local)) || isVideoExt(lowerExt(local))) {
					bm = mediaThumb(local, 96)
				}
				if bm != nil {
					ui.Image(c, bm).Fit(ui.Cover).Size(openFileIconBox, openFileIconBox)
				} else {
					ui.Box(c).Size(openFileIconBox, openFileIconBox).Center().Children(func() {
						a.fileIcon(c, Entry{Name: name, Ext: lowerExt(name), IsDir: dir}, 28)
					})
				}
			})
			if icon.Hovered() {
				icon.Background(colorHover())
			}
			if icon.Clicked() && local != "" {
				if dir {
					a.navigate(local)
				} else {
					a.openQuickLookPath(local)
				}
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c.Key("n"), name).FontSize(13).FontWeight(600).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0)
				meta := []string{}
				for _, s := range []string{size, when, it.Sub} {
					if s != "" {
						meta = append(meta, s)
					}
				}
				if len(meta) > 0 {
					ui.Text(c.Key("m"), strings.Join(meta, " · ")).FontSize(12).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0)
				}
			})
		})
	})
	if row.Hovered() {
		row.Background(colorHover())
	}
	components.Tooltip(c, row, it.Path)
	if row.DoubleClicked() && local != "" {
		if dir {
			a.navigate(local)
		} else {
			a.openSystemOpen(local, name)
		}
	}
}

func openStepColor(status string) ui.Color {
	switch status {
	case "done":
		return tokSuccess
	case "active":
		return colorAccent()
	case "error":
		return tokDanger
	}
	return colorMuted()
}

func (a *App) paintOpenSteps(c *ui.Context, v *ouiView) {
	ui.Column(c).FillWidth().MinWidth(0).Label("OpenUI steps").Children(func() {
		openMoreTitle(c, v.Text)
		for i, it := range v.Items {
			a.paintOpenStep(c.Key("s"+itoa(i)), it, i, i == len(v.Items)-1)
		}
	})
}

func (a *App) paintOpenStep(c *ui.Context, it *ouiView, i int, last bool) {
	tint := openStepColor(it.Variant)
	ui.Row(c).FillWidth().MinWidth(0).Gap(10).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c.Key("rail")).Width(22).Shrink(0).AlignItems(ui.Center).Children(func() {
			dot := ui.Box(c.Key("dot")).Size(22, 22).Radius(11).Center().Border(1.5, tint)
			if it.Variant == "done" || it.Variant == "active" || it.Variant == "error" {
				dot.Background(tint)
			}
			dot.Children(func() {
				switch it.Variant {
				case "done":
					components.Icon(c, lucide.Check()).Size(13, 13).TextColor(ui.Hex("#FFFFFF"))
				case "error":
					components.Icon(c, lucide.X()).Size(13, 13).TextColor(ui.Hex("#FFFFFF"))
				case "active":
					ui.Text(c, itoa(i+1)).FontSize(11).FontWeight(700).TextColor(ui.Hex("#FFFFFF"))
				default:
					ui.Text(c, itoa(i+1)).FontSize(11).FontWeight(600).TextColor(colorMuted())
				}
			})
			if !last {
				ui.Box(c.Key("line")).Width(1.5).Grow(1).MinHeight(8).Background(colorDivider())
			}
		})
		ui.Column(c.Key("body")).Grow(1).MinWidth(0).Gap(2).Padding(2, 0, 12, 0).Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				t := ui.Text(c.Key("t"), it.Text).FontSize(13).FontWeight(600).Wrap().Grow(1).MinWidth(0).Selectable()
				if it.Variant == "pending" {
					t.TextColor(colorMuted())
				} else {
					t.TextColor(colorText())
				}
				if it.Label != "" {
					ui.Text(c.Key("at"), it.Label).FontSize(11).TextColor(colorMuted()).Shrink(0)
				}
			})
			if it.Sub != "" {
				ui.Text(c.Key("b"), it.Sub).FontSize(12).LineHeight(1.4).TextColor(colorMuted()).Wrap().Selectable()
			}
		})
	})
}

func (a *App) paintOpenStats(c *ui.Context, v *ouiView) {
	ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(8).Label("OpenUI stats").Children(func() {
		for i, it := range v.Items {
			cc := c.Key("st" + itoa(i))
			tile := ui.Column(cc).Width(openStatTileW).Grow(1).MinWidth(0).Gap(3).Padding(10, 12).Radius(8).
				Border(1, colorDivider()).Background(colorSurface())
			tip := it.Label + "  " + it.Text
			if it.Sub != "" {
				tip += "  " + it.Sub
			}
			components.Tooltip(cc, tile, tip)
			tile.Children(func() {
				ui.Text(cc.Key("l"), it.Label).FontSize(12).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0)
				val := ui.Text(cc.Key("v"), it.Text).FontSize(20).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0).Selectable()
				if it.Tone != "" {
					val.TextColor(openToneColor(it.Tone))
				} else {
					val.TextColor(colorText())
				}
				if it.Sub != "" {
					d := ui.Text(cc.Key("d"), it.Sub).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0)
					if it.Tone != "" {
						d.TextColor(openToneColor(it.Tone))
					} else {
						d.TextColor(colorMuted())
					}
				}
			})
		}
	})
}

// openGalleryLayout returns the column count and tile width for a grid w
// wide. want > 0 is the program's count; it falls back to auto fit when
// that would leave tiles narrower than openGalleryMinW.
func openGalleryLayout(w float32, want, n int) (int, float32) {
	if w <= 0 {
		w = 4*openGalleryTileW + 3*openGalleryGap
	}
	cols := want
	if cols > 0 && (w-openGalleryGap*float32(cols-1))/float32(cols) < openGalleryMinW {
		cols = 0
	}
	if cols <= 0 {
		cols = int((w + openGalleryGap) / (openGalleryTileW + openGalleryGap))
		if cols < 1 {
			cols = 1
		}
		if n > 0 && cols > n {
			// Few items keep the default tile instead of stretching.
			return cols, openGalleryTileW
		}
	}
	tw := (w - openGalleryGap*float32(cols-1)) / float32(cols)
	return cols, float32(int(tw))
}

// openGalleryPending is a tile click waiting out the double-click window.
type openGalleryPending struct {
	path string
	at   time.Time
	// opened marks a double click already handled: the release that ends
	// it reports a click too, which must not queue a preview.
	opened bool
}

var openGalleryClick openGalleryPending

func (a *App) paintOpenGallery(c *ui.Context, v *ouiView) {
	ui.Column(c).FillWidth().MinWidth(0).Label("OpenUI gallery").Children(func() {
		openMoreTitle(c, v.Text)
		grid := ui.Row(c.Key("grid")).FillWidth().MinWidth(0).Wrap().Gap(openGalleryGap)
		_, tw := openGalleryLayout(grid.Bounds().W, v.Extra, len(v.Items))
		grid.Children(func() {
			for i, it := range v.Items {
				a.paintOpenGalleryTile(c.Key("g"+itoa(i)), it, tw)
			}
		})
	})
	if p := openGalleryClick; p.path != "" && !p.opened {
		if left := openClickDelay - openUINow().Sub(p.at); left <= 0 {
			openGalleryClick = openGalleryPending{}
			a.openQuickLookPath(p.path)
		} else {
			c.After(left)
		}
	}
}

func (a *App) paintOpenGalleryTile(c *ui.Context, it *ouiView, tw float32) {
	th := float32(int(tw * openGalleryTileH / openGalleryTileW))
	box := float32(256)
	if tw > 180 {
		box = 512
	}
	ui.Column(c).Width(tw).Shrink(0).Gap(3).Children(func() {
		local := openMediaLocal(it.Path)
		name := baseName(openMediaURLPath(it.Path))
		tile := ui.ButtonBase(c.Key("tile")).Size(tw, th).Radius(8).Clip().
			Border(1, colorDivider()).Background(colorAddress()).Label("预览 " + name)
		tile.Children(func() {
			var bm *ui.Bitmap
			switch {
			case strings.HasPrefix(it.Path, "data:image/"):
				bm, _ = formLoadBitmap(it.Path, int(box))
			case local != "":
				bm = mediaThumb(local, box)
			}
			if bm != nil {
				ui.Image(c, bm).Fit(ui.Cover).Size(tw, th)
			} else {
				ui.Box(c).Size(tw, th).Center().Children(func() {
					a.fileIcon(c, Entry{Name: name, Ext: lowerExt(name)}, min(36, th/2))
				})
			}
			if it.Variant == "video" {
				a.videoBadge(c, th)
			}
		})
		if tile.Hovered() || tile.Focused() {
			tile.Border(1.5, colorAccent().Alpha(0.7))
		}
		tip := name
		if it.Text != "" {
			tip = it.Text + "\n" + name
		}
		components.Tooltip(c, tile, tip)
		switch {
		case tile.DoubleClicked() && local != "":
			openGalleryClick = openGalleryPending{path: local, at: openUINow(), opened: true}
			a.openSystemOpen(local, name)
		case tile.Clicked() && local != "":
			if p := openGalleryClick; p.opened && p.path == local && openUINow().Sub(p.at) < openClickDelay {
				openGalleryClick = openGalleryPending{}
				break
			}
			openGalleryClick = openGalleryPending{path: local, at: openUINow()}
			c.After(openClickDelay)
		}
		if it.Text != "" {
			ui.Text(c.Key("cap"), it.Text).FontSize(11).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
}
