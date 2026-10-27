package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// home.go is the 主文件夹 page: Explorer's Home, one section of pinned
// 快速访问 folders and one of recently opened files.

// homeEntries returns the Home page's two sections, pins first. It is also
// flattened into the tab's entries so selection and the status bar see the
// same items the view renders.
func (a *App) homeEntries() (pins, recents []Entry) {
	for _, p := range a.pinnedPlaces() {
		pins = append(pins, Entry{Name: p.Name, Path: p.Path, IsDir: p.Kind != "file"})
	}
	for _, path := range a.store.recentList() {
		info, err := os.Lstat(path)
		if err != nil {
			continue // a recent that has since been deleted
		}
		e := Entry{
			Name:    filepath.Base(path),
			Path:    path,
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Dir:     filepath.Dir(path),
		}
		if !e.IsDir {
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		}
		recents = append(recents, e)
	}
	return pins, recents
}

// pinnedPlaces turns the persisted pins into sidebar/Home places, with the
// Windows folder names and icons where they are the standard ones.
func (a *App) pinnedPlaces() []Place {
	known := knownFolderNames()
	icons := map[string]string{
		"桌面": "desktop", "下载": "downloads", "文档": "documents",
		"图片": "pictures", "音乐": "music", "视频": "videos",
	}
	out := make([]Place, 0, len(a.store.pinnedList()))
	for _, path := range a.store.pinnedList() {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		name := filepath.Base(path)
		kind := "folder"
		if !info.IsDir() {
			// A file pinned from a drop stays in the list. Folders keep
			// the known-folder name and icon; a file is just itself.
			kind = "file"
		} else if n, ok := known[filepath.Clean(path)]; ok {
			name = n
		}
		if kind != "file" {
			if k, ok := icons[name]; ok {
				kind = k
			}
		}
		out = append(out, Place{Name: name, Path: path, Kind: kind})
	}
	return out
}

// openPinned opens a 快速访问 item. A folder is entered. A file has no
// listing of its own, so the pane opens the folder it sits in and selects
// it — the same thing the address bar does with a file path.
func (a *App) openPinned(path string, dir bool) {
	if dir {
		e := Entry{Name: filepath.Base(path), Path: path, IsDir: true}
		a.openDirEntry(e)
		return
	}
	a.cur().selectPath = path
	a.navigate(filepath.Dir(path))
}

// homeView renders the Home page: 快速访问 tiles, then 最近使用的文件.
func (a *App) homeView(c *ui.Context) {
	pins, recents := a.homeEntries()
	scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(8, 8, 12, 8)
	scroll.Children(func() {
		if len(pins) == 0 && len(recents) == 0 {
			a.paneMessage(c, "固定文件夹或打开文件后，这里会显示快速访问与最近使用的文件")
			return
		}
		a.homeSection(c, "快速访问")
		grid := ui.GridView(c, nil, len(pins), 190, 40, func(i int) {
			if i >= 0 && i < len(pins) {
				a.homePin(c, pins, i)
			}
		}).FillWidth().Shrink(0)
		a.bindPinDrop(grid)
		grid.Children(func() {
			if len(pins) == 0 {
				a.paneMessage(c, "把文件或文件夹拖到这里即可固定")
			}
		})
		a.homeSectionGap(c)
		a.homeSection(c, "最近使用的文件")
		list := ui.List(c, nil, len(recents), func(i int) {
			if i >= 0 && i < len(recents) {
				a.homeRecent(c, recents, i)
			}
		}).FillWidth().Shrink(0)
		list.Children(func() {
			if len(recents) == 0 {
				a.paneMessage(c, "尚无最近使用的文件")
			}
		})
	})
}

func (a *App) homeSection(c *ui.Context, title string) {
	row := ui.ButtonBase(c.Key("home-section-"+title)).FillWidth().Height(32).
		Padding(8, 4, 8, 6).AlignItems(ui.Center).Justify(ui.Start).Label(title)
	row.Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(colorText()).
			SingleLine()
	})
	// The heading is the pin target. A tile under it is still a folder:
	// a drop on the tile moves into that folder, a drop on the heading
	// or the gap around the tiles pins.
	if title == "快速访问" {
		a.bindPinDrop(row)
	}
}

func (a *App) homeSectionGap(c *ui.Context) {
	ui.Box(c).Height(14).FillWidth()
}

// homePin is one Quick Access tile: icon and name on one row. A click
// opens it — a folder is entered, a file is shown in its folder.
func (a *App) homePin(c *ui.Context, pins []Entry, i int) {
	e := pins[i]
	tile := ui.ButtonBase(c.Key("home-pin-"+e.Path)).Fill().Height(40).Radius(4).
		Padding(0, 8).Gap(10).AlignItems(ui.Center).Label(e.Name)
	if tile.Hovered() || a.isSelected(e.Path) {
		tile.Background(colorHover())
	}
	tile.Children(func() {
		glyph := ui.Box(c).Size(20, 20).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, e, 20)
		})
		ui.Text(c, e.Name).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if tile.Clicked() || tile.DoubleClicked() {
		a.openPinned(e.Path, e.IsDir)
	}
	if e.IsDir {
		a.bindDropFolder(c, tile, e.Path)
	}
	tile.ContextMenu(func(m *ui.Menu) {
		a.pinMenu(m, e)
	})
}

// homeRecent is one row of 最近使用的文件: icon, name, date and location.
func (a *App) homeRecent(c *ui.Context, recents []Entry, i int) {
	e := recents[i]
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(a.detailsRowH()).
		Radius(4).Padding(0, 8).Gap(10).AlignItems(ui.Center).Label(e.Name)
	a.paintSelect(row, e.Path)
	if row.Hovered() {
		row.Background(colorHover())
	}
	row.Children(func() {
		glyph := ui.Box(c).Size(16, 16).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, e, 16)
		})
		ui.Text(c, a.displayName(e)).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
		ui.Text(c, formatModTime(e.ModTime)).FontSize(12).TextColor(colorMuted()).
			Width(120).Shrink(0).SingleLine()
		ui.Text(c, strings.TrimPrefix(e.Dir, a.homePath+string(filepath.Separator))).
			FontSize(12).TextColor(colorMuted()).Width(200).Shrink(0).SingleLine()
	})
	if row.Clicked() {
		a.selectOnly(e.Path)
	}
	if row.DoubleClicked() {
		a.selectOnly(e.Path)
		a.doOpen()
	}
	a.bindItemInput(c, row, e)
	row.ContextMenu(func(m *ui.Menu) {
		a.itemMenu(m, e)
	})
}

// pinMenu is the Home tile's menu: open, unpin, properties.
func (a *App) pinMenu(m *ui.Menu, e Entry) {
	if m.Item("打开").Chosen() {
		a.openPinned(e.Path, e.IsDir)
	}
	if m.Item("从快速访问取消固定").Chosen() {
		if a.store.unpin(e.Path) {
			a.reload()
		}
	}
	m.Separator()
	if m.Item("属性").Chosen() {
		a.selectOnly(e.Path)
		a.showProperties()
	}
}

// displayName is the file's name with the extension hidden when
// 查看>显示>文件扩展名 is off, as Explorer hides known extensions.
func (a *App) displayName(e Entry) string {
	if a.showExt || e.IsDir || e.Ext == "" {
		return e.Name
	}
	ext := filepath.Ext(e.Name)
	if ext == "" || strings.EqualFold(strings.TrimPrefix(ext, "."), e.Ext) {
		return strings.TrimSuffix(e.Name, ext)
	}
	return e.Name
}

// recordRecent notes an opened file for the Home page, as Explorer's
// 最近使用的文件 does. macOS .app launches count too — Finder lists them
// under Recents — while ordinary folders stay out.
func (a *App) recordRecent(path string) {
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		if !isAppBundleName(filepath.Base(path)) {
			return
		}
	}
	a.store.pushRecent(path)
}
