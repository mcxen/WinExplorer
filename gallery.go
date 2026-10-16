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
	xdraw "golang.org/x/image/draw"
)

// gallery.go is Explorer's 画廊 (Gallery, Windows 11 23H2): the pictures
// of ~/Pictures and its subfolders, newest first, grouped by month, as
// real thumbnails — decoded in the background at bounded concurrency,
// cached small, evicted least-recently-used. Click selects, double-click
// opens, 空格 quick-previews, drag moves, exactly like any other item.

const (
	galleryThumbSide = 200  // px on the thumbnail's longest side
	galleryMaxPhotos = 2000 // scan bound
	galleryCap       = 400  // thumbnails kept in memory
	galleryWorkers   = 4    // concurrent decodes
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
	galleryInFlight = map[string]bool{}
)

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
		for len(galleryOrder) > galleryCap {
			oldest := galleryOrder[0]
			galleryOrder = galleryOrder[1:]
			delete(galleryByPath, oldest)
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
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if scale := float64(side) / float64(max(w, h)); scale < 1 {
		tw, th := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		img = dst
	}
	return ui.NewBitmap(img), nil
}

// galleryView renders the 画廊 page: month-sectioned grids of
// thumbnails, newest month first.
func (a *App) galleryView(c *ui.Context) {
	st := galleryScan.Load()
	scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(8, 8, 12, 8)
	scroll.Children(func() {
		if st == nil || !st.loaded {
			a.paneMessage(c, "正在扫描图片…")
			return
		}
		photos := st.photos
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
			grid := ui.GridView(c, nil, end-i, 120, 120, func(j int) {
				if j >= 0 && j < end-i {
					a.galleryTile(c, photos[i+j])
				}
			}).FillWidth().Shrink(0)
			grid.Children(func() {})
			i = end
		}
	})
	a.bindPaneMenu(c, scroll)
}

// galleryTile is one thumbnail: the picture fills the tile over a quiet
// frame until it decodes, and the item behaves like any other file —
// select, open, quick preview, drag, context menu.
func (a *App) galleryTile(c *ui.Context, p galleryPhoto) {
	tile := ui.ButtonBase(c.Key(p.path)).Size(112, 112).Shrink(0).Radius(4).
		Clip().Label(p.name)
	switch {
	case a.isSelected(p.path):
		tile.Background(colorSelected())
	case tile.Hovered():
		tile.Background(colorHover())
	default:
		tile.Background(colorHover().Alpha(0.35))
	}
	tile.Children(func() {
		if bm := galleryThumb(p.path); bm != nil {
			ui.Image(c, bm).Fit(ui.Cover)
			return
		}
		ui.Box(c).Fill()
	})
	if tile.Clicked() {
		a.selectOnly(p.path)
	}
	if tile.DoubleClicked() {
		a.selectOnly(p.path)
		a.doOpen()
	}
	tile.Drag(a.dragValueFor(p.path))
	tile.ContextMenu(func(m *ui.Menu) {
		a.itemMenu(m, galleryEntry(p))
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
