package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// thumb.go is the file pane's media thumbnails: in the icon views a
// picture or video shows its own frame, and a folder holding pictures or
// videos shows up to four of them inside its own folder shape — Explorer's
// folder thumbnails, where the pictures fill the folder's front panel but
// the folder stays a folder. Decoding runs in the background at bounded
// concurrency, and the results are cached, evicted least-recently-used.
//
// The size a thumbnail is decoded at follows the tile: a small tile asks
// for a small picture, and 放大 past a step decodes it again, larger.

const (
	thumbWorkers = 4 // concurrent decodes

	// thumbBudget is how many bytes of decoded pictures the cache holds
	// before the oldest are dropped: a 512 px cover is 1 MB, and the tile
	// sizes reach it, so a count alone would let the cache grow without
	// bound. The budget also covers the halved copies a bitmap keeps for
	// drawing itself smaller, which add a third again.
	thumbBudget = 64 << 20

	// thumbMaxEntries bounds the entries themselves, so a tree of folders
	// that turned out to hold no media cannot fill the map with misses.
	thumbMaxEntries = 2000

	// thumbMinBox is the smallest tile that gets a thumbnail. Below it —
	// the details and list views, at 16 px — the drawn glyphs are both
	// cheaper and clearer.
	thumbMinBox = 48

	// coverCells is the most media files one folder cover shows.
	coverCells = 4

	// folderPanel* place a media folder's pictures in its drawn folder:
	// the front panel of icons.go's drawFolder, inset so the folder's own
	// edge still frames them, with a corner radius a little tighter than
	// the folder's.
	folderPanelX, folderPanelY = 0.06, 0.28
	folderPanelW, folderPanelH = 0.88, 0.58
	folderPanelInset           = 0.022
	folderPanelMinInset        = 2
	folderBodyRadius           = 0.08
)

// imageExts are the extensions the file pane treats as pictures;
// stdlibImageExts, a subset, are the ones image.Decode reads directly.
// The rest (HEIC, AVIF, TIFF, ICO) go through Quick Look.
var (
	imageExts = map[string]bool{
		"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "bmp": true,
		"heic": true, "heif": true, "tif": true, "tiff": true, "avif": true, "ico": true,
	}
	stdlibImageExts = map[string]bool{
		"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "bmp": true,
	}
	videoExts = map[string]bool{
		"mp4": true, "m4v": true, "mov": true, "mkv": true, "avi": true,
		"webm": true, "wmv": true, "flv": true,
	}
)

func isImageExt(ext string) bool { return imageExts[ext] }
func isVideoExt(ext string) bool { return videoExts[ext] }
func isMediaExt(ext string) bool { return imageExts[ext] || videoExts[ext] }

func lowerExt(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// thumbCacheKey names one cached picture: "f:" a media file, "d:" a
// folder's cover.
type thumbCacheKey string

// thumbEntry is what the cache holds for a key: the picture at the size it
// was decoded for, or nothing when there was nothing to decode. bytes is
// what bm costs, so the cache can hold to a budget.
type thumbEntry struct {
	bm    *ui.Bitmap
	side  int
	bytes int
}

var (
	thumbMu    sync.Mutex
	thumbCache = map[thumbCacheKey]thumbEntry{}
	thumbOrder []thumbCacheKey
	thumbBytes int
	thumbBusy  = map[thumbCacheKey]bool{}
	thumbSem   = make(chan struct{}, thumbWorkers)
)

// bitmapBytes is what a decoded picture costs: four bytes a pixel. The
// halved copies it keeps for drawing itself smaller are not counted, and
// add a third again at most.
func bitmapBytes(bm *ui.Bitmap) int {
	if bm == nil {
		return 0
	}
	w, h := bm.Size()
	return w * h * 4
}

// thumbSide rounds a tile's box up to the size its picture is decoded at,
// so the four sizes the views reach share four decode sizes.
func thumbSide(box float32) int {
	switch {
	case box <= 64:
		return 64
	case box <= 128:
		return 128
	case box <= 256:
		return 256
	default:
		return 512
	}
}

// mediaThumb returns the thumbnail of a picture or video, or nil while it
// is still being decoded or when the file is neither. box is the tile's
// side in DIPs.
func mediaThumb(path string, box float32) *ui.Bitmap {
	ext := lowerExt(path)
	if !isMediaExt(ext) {
		return nil
	}
	side := thumbSide(box)
	return cachedThumb("f:"+thumbCacheKey(path), side, func() image.Image {
		img, err := decodeThumbImage(path, side)
		if err != nil {
			return nil
		}
		return img
	})
}

// folderCoverThumb returns the pictures a media folder shows, tiled into
// the shape of its front panel: up to four of its pictures and videos.
// nil means there is no cover this frame — it is being made, or the folder
// holds no media.
func folderCoverThumb(dir string, box float32) *ui.Bitmap {
	side := thumbSide(box)
	panel := folderCoverPanel(box)
	// The cover is made in the panel's shape rather than square, so what
	// gets composed is what shows: a square cover would have its sides
	// cropped away by the panel.
	w := side
	h := max(2, int(float32(side)*panel.H/panel.W+0.5))
	return cachedThumb("d:"+thumbCacheKey(dir), side, func() image.Image {
		paths := folderMedia(dir, coverCells)
		if len(paths) == 0 {
			return nil
		}
		pics := make([]image.Image, 0, len(paths))
		for _, p := range paths {
			if img, err := decodeThumbImage(p, side); err == nil {
				pics = append(pics, img)
			}
		}
		if len(pics) == 0 {
			return nil
		}
		return composeCover(pics, w, h)
	})
}

// cachedThumb returns the picture cached under key at side px, asking load
// for it when there is none and a worker is free. nil means there is
// nothing to show this frame — a decode just started, the file has nothing
// to decode, or every worker was busy — so the tile keeps its drawn glyph.
// A picture that is there but too small for the tile is still handed back
// while a bigger one is decoded, rather than leaving the tile blank.
func cachedThumb(key thumbCacheKey, side int, load func() image.Image) *ui.Bitmap {
	thumbMu.Lock()
	e, cached := thumbCache[key]
	if cached {
		touchThumbKey(key)
		if e.side >= side || thumbBusy[key] {
			// Big enough already, nothing to make, or a bigger one is on
			// its way: what there is, is what shows.
			thumbMu.Unlock()
			return e.bm
		}
	} else if thumbBusy[key] {
		thumbMu.Unlock()
		return nil
	}
	thumbBusy[key] = true
	thumbMu.Unlock()

	select {
	case thumbSem <- struct{}{}:
	default:
		// Every worker is busy; the next frame asks again.
		thumbMu.Lock()
		delete(thumbBusy, key)
		thumbMu.Unlock()
		return e.bm
	}

	go func() {
		img := load()
		<-thumbSem
		thumbMu.Lock()
		delete(thumbBusy, key)
		if img == nil {
			thumbCache[key] = thumbEntry{side: side}
			touchThumbKey(key)
		} else {
			bm := ui.NewBitmap(img)
			if old, ok := thumbCache[key]; ok {
				thumbBytes -= old.bytes
			}
			entry := thumbEntry{bm: bm, side: side, bytes: bitmapBytes(bm)}
			thumbCache[key] = entry
			thumbBytes += entry.bytes
			touchThumbKey(key)
			trimThumbs()
		}
		thumbMu.Unlock()
		repaint()
	}()
	// The picture is on its way; what was there, if anything, shows first.
	return e.bm
}

// touchThumbKey moves key to the young end of the eviction order, adding it
// when it has none. The caller holds thumbMu.
func touchThumbKey(key thumbCacheKey) {
	for i, k := range thumbOrder {
		if k == key {
			thumbOrder = append(thumbOrder[:i], thumbOrder[i+1:]...)
			thumbOrder = append(thumbOrder, key)
			return
		}
	}
	thumbOrder = append(thumbOrder, key)
}

// trimThumbs drops the oldest entries until the cache is back inside its
// budget. An entry that turned out to have no picture counts nothing
// against it, but is still dropped with the rest once the cache is full:
// finding out again is one directory read.
func trimThumbs() {
	for (thumbBytes > thumbBudget || len(thumbCache) > thumbMaxEntries) && len(thumbOrder) > 1 {
		oldest := thumbOrder[0]
		thumbOrder = thumbOrder[1:]
		if e, ok := thumbCache[oldest]; ok {
			thumbBytes -= e.bytes
			delete(thumbCache, oldest)
		}
	}
}

// clearThumbs forgets every cached picture, so 刷新 re-reads folders and
// files that changed.
func clearThumbs() {
	thumbMu.Lock()
	thumbCache = map[thumbCacheKey]thumbEntry{}
	thumbOrder = nil
	thumbBytes = 0
	thumbMu.Unlock()
}

// decodeThumbImage decodes one media file to a picture no bigger than side
// px on its longest edge: pictures with the stdlib, the formats it cannot
// read and videos through Quick Look and ffmpeg.
func decodeThumbImage(path string, side int) (image.Image, error) {
	ext := lowerExt(path)
	if isVideoExt(ext) {
		return videoPosterImage(path, side)
	}
	if stdlibImageExts[ext] {
		if img, err := decodeImageScaled(path, side); err == nil {
			return img, nil
		}
	}
	// Quick Look renders its thumbnail at twice the side so the GPU has
	// something to sample down from.
	return quickLookImage(path, side*2)
}

// folderMedia names up to max pictures and videos directly inside dir, in
// name order, pictures first so a mixed folder leads with them.
func folderMedia(dir string, max int) []string {
	if max <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var images, videos []string
	for _, d := range entries {
		if len(images) >= max {
			break
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || d.IsDir() {
			continue
		}
		switch ext := lowerExt(name); {
		case isImageExt(ext):
			images = append(images, filepath.Join(dir, name))
		case isVideoExt(ext):
			if len(videos) < max {
				videos = append(videos, filepath.Join(dir, name))
			}
		}
	}
	out := append(images, videos...)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// composeCover tiles pictures into one rectangle, the way Explorer tiles a
// folder's thumbnail: one fills it, two split it into halves, three put
// one beside two stacked, four or more make a 2×2. Cells are a hair apart,
// and land on a transparent background, so the tile's own colour shows in
// the seams.
func composeCover(pics []image.Image, w, h int) image.Image {
	w, h = max(w, 2), max(h, 2)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	hw, hh := w/2, h/2
	var cells []image.Rectangle
	switch len(pics) {
	case 1:
		cells = []image.Rectangle{image.Rect(0, 0, w, h)}
	case 2:
		cells = []image.Rectangle{
			image.Rect(0, 0, hw, h),
			image.Rect(hw, 0, w, h),
		}
	case 3:
		cells = []image.Rectangle{
			image.Rect(0, 0, hw, h),
			image.Rect(hw, 0, w, hh),
			image.Rect(hw, hh, w, h),
		}
	default:
		cells = []image.Rectangle{
			image.Rect(0, 0, hw, hh),
			image.Rect(hw, 0, w, hh),
			image.Rect(0, hh, hw, h),
			image.Rect(hw, hh, w, h),
		}
	}
	for i, cell := range cells {
		if i >= len(pics) {
			break
		}
		drawCover(dst, cell.Inset(1), pics[i])
	}
	return dst
}

// folderCoverPanel is where a media folder's pictures sit inside its tile:
// the folder's front panel, inset so the folder's own edge still frames
// them and its tab stays clear. The numbers follow drawFolder's body, so a
// folder with pictures is drawn as any other folder is.
func folderCoverPanel(box float32) ui.Rect {
	inset := folderCoverPanelInset(box)
	return ui.Rect{
		X: box*folderPanelX + inset,
		Y: box*folderPanelY + inset,
		W: box*folderPanelW - 2*inset,
		H: box*folderPanelH - 2*inset,
	}
}

// folderCoverPanelInset is the gap between the folder's edge and its
// pictures. It is proportional, but never thinner than a couple of DIPs:
// in the medium tiles a purely proportional gap rounds away to nothing and
// the folder would read as a bare collage, which is the one thing it must
// not look like.
func folderCoverPanelInset(box float32) float32 {
	return max(box*folderPanelInset, folderPanelMinInset)
}

// folderCoverPanelRadius rounds the pictures' corners a little tighter
// than the folder body's, so they sit inside the body's own curve instead
// of squaring off its corners.
func folderCoverPanelRadius(box float32) float32 {
	return max(box*folderBodyRadius-folderCoverPanelInset(box), 1)
}

// drawCover scales src into r, keeping its aspect ratio and cropping what
// does not fit, as Fit(Cover) does in the views.
func drawCover(dst *image.RGBA, r image.Rectangle, src image.Image) {
	if src == nil || r.Dx() <= 0 || r.Dy() <= 0 {
		return
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	rw, rh := r.Dx(), r.Dy()
	sr := sb
	if sw*rh > sh*rw { // wider than the cell: crop the sides
		cw := sh * rw / rh
		x0 := sb.Min.X + (sw-cw)/2
		sr = image.Rect(x0, sb.Min.Y, x0+cw, sb.Max.Y)
	} else { // taller than the cell: crop the ends
		ch := sw * rh / rw
		y0 := sb.Min.Y + (sh-ch)/2
		sr = image.Rect(sb.Min.X, y0, sb.Max.X, y0+ch)
	}
	xdraw.CatmullRom.Scale(dst, r, src, sr, xdraw.Over, nil)
}
