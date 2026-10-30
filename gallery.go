package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// gallery.go is Explorer's 画廊 (Gallery, Windows 11 23H2): the pictures
// of ~/Pictures and its subfolders, newest first, grouped by month, as
// real thumbnails — decoded in the background at bounded concurrency,
// cached small, evicted least-recently-used. Click selects, double-click
// opens, 空格 quick-previews, drag moves, exactly like any other item.

const (
	galleryThumbSide = 200      // px on the thumbnail's longest side
	galleryMaxPhotos = 2000     // scan bound
	galleryBudget    = 32 << 20 // bytes of thumbnails kept in memory
	galleryMaxEntry  = 600      // entries, for pictures that fail to decode
	galleryWorkers   = 4        // concurrent decodes
	galleryCellSide  = 120      // a month grid's cell, the tile's 112 px inside
	galleryScrollPad = 20       // the page scroll's padding, above and below
	// galleryCellH is a cell's height: the square picture, then its name
	// and date under it.
	galleryCellH = galleryCellSide + 34
)

// galleryPhoto is one picture the gallery shows.
type galleryPhoto struct {
	path    string
	name    string
	modTime time.Time
	size    int64
}

// galleryState is the loaded scan, replaced atomically when it lands.
type galleryState struct {
	photos []galleryPhoto
	loaded bool
}

var (
	galleryScan atomic.Pointer[galleryState]
	gallerySem  = make(chan struct{}, galleryWorkers)
	galleryN    atomic.Int32 // thumbnails landed since the last repaint

	galleryMu       sync.Mutex
	galleryOrder    []string
	galleryByPath   = map[string]*ui.Bitmap{}
	galleryBytes    int
	galleryInFlight = map[string]bool{}
)

// galleryPluginID is the plugin's id in the registry (plugins.go).
const galleryPluginID = "gallery"

// registerGalleryPlugin registers the 画廊 plugin: the sidebar's gallery
// place. Switched off, the pane does not list it; the page itself keeps a
// note (galleryView's 插件已停用).
func registerGalleryPlugin() {
	RegisterPlugin(Plugin{
		ID:   galleryPluginID,
		Name: "画廊",
		Desc: "侧边栏的图片画廊页",
		NavItems: func(a *App) []PluginNavItem {
			return []PluginNavItem{{
				Key:   "nav-gallery",
				Label: "画廊",
				Kind:  "gallery",
				Path:  pathGallery,
				Run: func() {
					a.navigate(pathGallery)
				},
			}}
		},
	})
}

// galleryExts are the extensions the gallery collects.
var galleryExts = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true,
	"bmp": true, "heic": true, "tif": true, "tiff": true, "avif": true, "ico": true,
}

// startGalleryScan walks ~/Pictures once in the background and publishes
// the photo list; a later call is a no-op once results exist.
func startGalleryScan() {
	if st := galleryScan.Load(); st != nil {
		return // already scanned or scanning
	}
	refreshGallery()
}

// refreshGallery rescans ~/Pictures and republishes, used when the
// gallery's own files change through the app.
func refreshGallery() {
	galleryScan.Store(&galleryState{})
	go func() {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			galleryScan.Store(&galleryState{loaded: true})
			return
		}
		photos := scanGallery(filepath.Join(home, "Pictures"))
		galleryScan.Store(&galleryState{photos: photos, loaded: true})
		repaint()
	}()
}

// scanGallery collects the picture files under root, newest first,
// skipping hidden folders and the Photos app's bundle, bounded so a huge
// library cannot wedge the scan.
func scanGallery(root string) []galleryPhoto {
	var photos []galleryPhoto
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// The Photos app keeps its library in one bundle; it is Finder's
		// to open.
		if d.IsDir() && strings.HasSuffix(name, ".photoslibrary") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if !galleryExts[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		photos = append(photos, galleryPhoto{path: path, name: name, modTime: info.ModTime(), size: info.Size()})
		if len(photos) >= galleryMaxPhotos {
			return filepath.SkipAll
		}
		return nil
	})
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i].modTime.After(photos[j].modTime)
	})
	return photos
}

// monthKeyOf is the grouping label, e.g. "2026年10月".
func monthKeyOf(t time.Time) string {
	if t.IsZero() {
		return "未知时间"
	}
	return fmt.Sprintf("%d年%d月", t.Year(), int(t.Month()))
}

// galleryThumb returns the cached thumbnail, launching a background
// decode when absent.
func galleryThumb(path string) *ui.Bitmap {
	galleryMu.Lock()
	bm := galleryByPath[path]
	if bm != nil {
		touchThumb(path)
	}
	busy := galleryInFlight[path]
	galleryMu.Unlock()
	if bm == nil && !busy {
		go loadGalleryThumb(path)
	}
	return bm
}

func touchThumb(path string) {
	for i, k := range galleryOrder {
		if k == path {
			galleryOrder = append(galleryOrder[:i], galleryOrder[i+1:]...)
			galleryOrder = append(galleryOrder, path)
			return
		}
	}
}

func loadGalleryThumb(path string) {
	galleryMu.Lock()
	galleryInFlight[path] = true
	galleryMu.Unlock()
	gallerySem <- struct{}{}
	defer func() { <-gallerySem }()
	bm, err := decodeThumb(path, galleryThumbSide)
	galleryMu.Lock()
	delete(galleryInFlight, path)
	if err == nil && bm != nil {
		galleryByPath[path] = bm
		galleryOrder = append(galleryOrder, path)
		galleryBytes += bitmapBytes(bm)
		// The cache holds to a byte budget, not a picture count: thumbnails
		// differ in size, and 400 of the largest would be tens of MB. The
		// entry cap only bounds the map itself.
		for len(galleryOrder) > 1 &&
			(galleryBytes > galleryBudget || len(galleryOrder) > galleryMaxEntry) {
			oldest := galleryOrder[0]
			galleryOrder = galleryOrder[1:]
			if old, ok := galleryByPath[oldest]; ok {
				galleryBytes -= bitmapBytes(old)
				delete(galleryByPath, oldest)
			}
		}
	}
	galleryMu.Unlock()
	if err == nil && galleryN.Add(1)%8 == 0 {
		repaint()
	}
}

// decodeThumb loads one picture as a ui.Bitmap scaled to fit side px on
// its longest edge: the stdlib formats decode and shrink with Catmull-Rom
// — a 4K photo costs a thumbnail's memory — and HEIC/TIFF/AVIF go
// through Quick Look, which renders a thumbnail directly.
func decodeThumb(path string, side int) (*ui.Bitmap, error) {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "ico":
		return decodeScaled(path, side)
	}
	return quickLookThumb(path, side*2)
}

// decodeScaled decodes with the stdlib (the ui package registers
// png/jpeg/gif/webp/bmp) and shrinks with Catmull-Rom.
func decodeScaled(path string, side int) (*ui.Bitmap, error) {
	img, err := decodeImageScaled(path, side)
	if err != nil {
		return nil, err
	}
	return ui.NewBitmap(img), nil
}

// decodeImageScaled is decodeScaled as the decoded picture, for callers
// that compose several into one cover. imgsize.go owns the reduction.
func decodeImageScaled(path string, side int) (image.Image, error) {
	return decodeFileCached(path, side)
}

// galleryView renders the 画廊 page: month-sectioned grids of
// thumbnails, newest month first.
func (a *App) galleryView(c *ui.Context) {
	scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(8, 8, 12, 8)
	scroll.Children(func() {
		// The gallery plugin owns the page: switched off, the pane keeps
		// a quiet note instead of the scan, reachable only through a
		// back/forward history the hidden place left behind.
		if !a.pluginEnabled(galleryPluginID) {
			a.paneMessage(c, "插件已停用")
			return
		}
		st := galleryScan.Load()
		if st == nil || !st.loaded {
			a.paneMessage(c, "正在扫描图片…")
			return
		}
		photos := make([]galleryPhoto, 0, len(st.photos))
		for _, ph := range st.photos {
			if !aiFx.look(ph.path).gone {
				photos = append(photos, ph)
			}
		}
		if len(photos) == 0 {
			a.paneMessage(c, "“图片”文件夹中没有图片")
			return
		}
		i := 0
		for i < len(photos) {
			key := monthKeyOf(photos[i].modTime)
			end := i
			for end < len(photos) && monthKeyOf(photos[end].modTime) == key {
				end++
			}
			a.homeSection(c, fmt.Sprintf("%s（%d 张）", key, end-i))
			grid := ui.GridView(c, nil, end-i, galleryCellSide, galleryCellH, func(j int) {
				if j >= 0 && j < end-i {
					a.galleryTile(c, photos[i+j])
				}
			}).FillWidth().Shrink(0)
			grid.Children(func() {})
			// The month grids share the pane's width, so the arrows step by
			// their columns, whatever 查看 mode the page was left in.
			if b := grid.Bounds(); b.W > 0 {
				a.cur().gridCols = gridCols(c, b.W, galleryCellSide, 0)
			}
			i = end
		}
		// The page's scroll says how many rows of tiles it shows, which
		// Page Up and Page Down move by.
		if b := scroll.Bounds(); b.H > 0 {
			a.cur().gridRows = max(int((b.H-galleryScrollPad)/galleryCellH), 1)
		}
	})
	a.bindPaneMenu(c, scroll)
}

// galleryTile is one thumbnail: the picture fills the tile over a quiet
// frame until it decodes, and the item behaves like any other file —
// select, open, quick preview, drag, context menu.
func (a *App) galleryTile(c *ui.Context, p galleryPhoto) {
	tile := ui.ButtonBase(c.Key(p.path)).Size(112, galleryCellH-8).Shrink(0).Radius(4).
		Clip().Label(p.name)
	t := a.cur()
	if t.itemBounds == nil {
		t.itemBounds = map[string]ui.Rect{}
	}
	t.itemBounds[p.path] = tile.Bounds()
	switch {
	case a.isSelected(p.path):
		tile.Background(colorSelected())
	case tile.Hovered():
		tile.Background(colorHover())
	}
	aiFxRow(c, tile, p.path, 0, 4)
	tile.Children(func() {
		ui.Column(c).Fill().Gap(3).Children(func() { a.galleryTileBody(c, p) })
	})
	if tile.Clicked() {
		a.selectOnly(p.path)
	}
	// The Edit menu's commands arrive at the focused tile on macOS, as they
	// do at a row of the other views (files.go's bindItemInput).
	tile.HandleInput(a.paneCommand)
	if tile.DoubleClicked() {
		a.selectOnly(p.path)
		a.doOpen()
	}
	a.bindDragSource(tile, p.path)
	a.contextMenu(c, tile, func(m *xMenu) {
		a.itemMenu(m, galleryEntry(p))
	})
	// The month grids are rows inside one plain Scroll, so every tile is
	// built: the one the keyboard moved to raises itself into view.
	if a.cur().reveal == p.path {
		tile.ScrollIntoView()
	}
}

// galleryTileBody is a tile's picture with its name and date under it.
func (a *App) galleryTileBody(c *ui.Context, p galleryPhoto) {
	// The picture fills its square edge to edge; only a picture still
	// decoding shows the quiet frame, so no grey band is left under a
	// landscape shot.
	pic := ui.Box(c).Size(112, 112).Shrink(0).Radius(4).Clip()
	if galleryThumb(p.path) == nil {
		pic.Background(colorHover().Alpha(0.35))
	}
	pic.Children(func() {
		if bm := galleryThumb(p.path); bm != nil {
			ui.Image(c, bm).Fill().Fit(ui.Cover)
		}
	})
	ui.Column(c).FillWidth().Padding(0, 4).Gap(1).Children(func() {
		ui.Text(c, p.name).FontSize(11).TextColor(colorText()).SingleLine().FillWidth()
		ui.Text(c, p.modTime.Format("2006/01/02")).FontSize(10).TextColor(colorMuted()).SingleLine()
	})
}

// galleryEntry is the file pane's Entry for a picture, so every
// operation — copy, compress, preview, 打开方式, extensions — applies.
// It maps the scan's cached facts; no per-frame stat.
func galleryEntry(p galleryPhoto) Entry {
	return Entry{
		Name:    p.name,
		Path:    p.path,
		Ext:     strings.ToLower(strings.TrimPrefix(filepath.Ext(p.name), ".")),
		Dir:     filepath.Dir(p.path),
		IsDir:   false,
		Size:    p.size,
		ModTime: p.modTime,
	}
}
