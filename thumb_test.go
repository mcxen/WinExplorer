package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// solidPNG writes a side×side PNG of one colour and returns its path.
func solidPNG(t *testing.T, path string, side int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

func TestThumbSide(t *testing.T) {
	cases := []struct {
		box  float32
		want int
	}{
		{16, 64}, {32, 64}, {48, 64}, {64, 64},
		{72, 128}, {96, 128}, {128, 128},
		{150, 256}, {256, 256},
		{384, 512}, {512, 512},
	}
	for _, c := range cases {
		if got := thumbSide(c.box); got != c.want {
			t.Errorf("thumbSide(%v) = %d, want %d", c.box, got, c.want)
		}
	}
}

func TestMediaExts(t *testing.T) {
	for _, ext := range []string{"png", "jpg", "heic", "tiff", "avif"} {
		if !isImageExt(ext) {
			t.Errorf("%s should be a picture", ext)
		}
	}
	for _, ext := range []string{"mp4", "mov", "mkv"} {
		if !isVideoExt(ext) || !isMediaExt(ext) {
			t.Errorf("%s should be a video", ext)
		}
	}
	if isMediaExt("txt") || isImageExt("mp3") {
		t.Error("text and audio are not media for the tiles")
	}
	// The stdlib handles a subset; the rest go through Quick Look.
	if !stdlibImageExts["png"] || stdlibImageExts["heic"] || stdlibImageExts["ico"] {
		t.Error("stdlib set is wrong")
	}
}

func TestIsVolumeRoot(t *testing.T) {
	if !isVolumeRoot("/") {
		t.Error("/ is a volume root")
	}
	if isVolumeRoot("/Users") || isVolumeRoot("/Users/mcx") {
		t.Error("a folder inside a volume is not a volume root")
	}
}

func TestFolderMedia(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-fm")
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "pics"), 0o755)
	for _, name := range []string{"a.png", "b.jpg", "c.txt", ".hidden.png", "d.webp", "e.mp4"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "pics", "inside.png"), []byte("x"), 0o644)

	got := folderMedia(dir, 4)
	if len(got) != 4 {
		t.Fatalf("folderMedia = %v", got)
	}
	// Pictures first, in name order, no hidden file.
	for i, want := range []string{"a.png", "b.jpg", "d.webp"} {
		if filepath.Base(got[i]) != want {
			t.Fatalf("cell %d = %s, want %s (%v)", i, filepath.Base(got[i]), want, got)
		}
	}
	// The video fills the last cell; nothing comes from the subfolder.
	if filepath.Base(got[3]) != "e.mp4" {
		t.Fatalf("last cell = %s, want e.mp4 (%v)", filepath.Base(got[3]), got)
	}

	// A folder of nothing but media bounds at max.
	if len(folderMedia(dir, 2)) != 2 {
		t.Fatal("max is not honoured")
	}
	// An empty folder, and one that is not there.
	if got := folderMedia(filepath.Join(dir, "pics"), 4); len(got) != 1 {
		t.Fatalf("subfolder media = %v", got)
	}
	if got := folderMedia(filepath.Join(dir, "nope"), 4); got != nil {
		t.Fatalf("missing folder = %v", got)
	}
}

func TestComposeCover(t *testing.T) {
	solid := func(c color.RGBA) image.Image {
		img := image.NewRGBA(image.Rect(0, 0, 40, 40))
		for y := 0; y < 40; y++ {
			for x := 0; x < 40; x++ {
				img.Set(x, y, c)
			}
		}
		return img
	}
	const side = 64
	// One picture fills the cover, two split it into halves, three put one
	// beside two stacked, four make a 2×2. The middle of every cell is
	// painted in each layout.
	cells := map[int][]image.Point{
		1: {{32, 32}},
		2: {{16, 32}, {48, 32}},
		3: {{16, 32}, {48, 16}, {48, 48}},
		4: {{16, 16}, {48, 16}, {16, 48}, {48, 48}},
	}
	for n := 1; n <= 4; n++ {
		pics := make([]image.Image, n)
		for i := range pics {
			pics[i] = solid(color.RGBA{uint8(40 * (i + 1)), 0x80, 0x10, 255})
		}
		cover := composeCover(pics, side)
		if b := cover.Bounds(); b.Dx() != side || b.Dy() != side {
			t.Fatalf("%d pictures: cover = %v", n, b)
		}
		for i, p := range cells[n] {
			if _, _, _, a := cover.At(p.X, p.Y).RGBA(); a == 0 {
				t.Fatalf("%d pictures: cell %d at %v is empty", n, i, p)
			}
		}
	}
	// A cover asks for at least two pixels a side, so the cells never
	// collapse.
	if b := composeCover([]image.Image{solid(color.RGBA{1, 2, 3, 255})}, 1).Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("tiny cover = %v", b)
	}
}

func TestComposeCoverLayout(t *testing.T) {
	red := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			red.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	blue := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			blue.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	const side = 64
	// Two pictures split the cover into left and right halves.
	cover := composeCover([]image.Image{red, blue}, side)
	if r, _, _, _ := cover.At(16, 32).RGBA(); r < 0xf000 {
		t.Fatalf("left half should be red, got %v", cover.At(16, 32))
	}
	if _, _, b, _ := cover.At(48, 32).RGBA(); b < 0xf000 {
		t.Fatalf("right half should be blue, got %v", cover.At(48, 32))
	}
	// Three pictures stack the last two on the right.
	cover = composeCover([]image.Image{red, blue, red}, side)
	if r, _, _, _ := cover.At(16, 16).RGBA(); r < 0xf000 {
		t.Fatalf("left half should be red, got %v", cover.At(16, 16))
	}
	if _, _, b, _ := cover.At(48, 16).RGBA(); b < 0xf000 {
		t.Fatalf("top right should be blue, got %v", cover.At(48, 16))
	}
}

func TestMediaThumbLifecycle(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-mt")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "pic.png")
	solidPNG(t, path, 8, color.RGBA{0, 255, 0, 255})

	if bm := mediaThumb(path, 96); bm != nil {
		t.Fatal("cache should start empty")
	}
	for i := 0; i < 200; i++ {
		if bm := mediaThumb(path, 96); bm != nil {
			if w, h := bm.Size(); w != 8 || h != 8 {
				t.Fatalf("thumb = %dx%d", w, h)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("thumbnail never landed")
}

func TestMediaThumbIgnoresNonMedia(t *testing.T) {
	if bm := mediaThumb("/tmp/whatever.txt", 96); bm != nil {
		t.Fatal("a text file has no thumbnail")
	}
	if bm := mediaThumb("/tmp/whatever.mp3", 96); bm != nil {
		t.Fatal("audio keeps its drawn glyph")
	}
}

// TestMediaThumbZoomRereads covers 放大 past a decode step: the tile asks
// again at 512, and the smaller picture is not handed back for it.
func TestMediaThumbZoomRereads(t *testing.T) {
	clearThumbs()
	defer clearThumbs()
	dir, _ := os.MkdirTemp("", "we-mtz")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "pic.png")
	solidPNG(t, path, 300, color.RGBA{200, 100, 0, 255})

	// Land the 128 px tier first, which is what an unzoomed 超大图标 asks
	// for. The picture is bigger than the tier, so it is scaled to it.
	var small *ui.Bitmap
	for i := 0; i < 200 && small == nil; i++ {
		small = mediaThumb(path, 96)
		time.Sleep(20 * time.Millisecond)
	}
	if small == nil {
		t.Fatal("the 128 px thumbnail never landed")
	}
	if w, _ := small.Size(); w != 128 {
		t.Fatalf("128 tier thumb = %d px wide", w)
	}

	// At 4× the tile is 384 px, which asks for the 512 tier. The smaller
	// picture is not enough, so a fresh decode starts — and until it lands
	// the tile keeps showing the smaller one rather than going blank.
	if bm := mediaThumb(path, 384); bm == nil {
		t.Fatal("the tile should keep the smaller picture while a bigger one decodes")
	}
	var big *ui.Bitmap
	for i := 0; i < 200; i++ {
		big = mediaThumb(path, 384)
		if big != nil && big != small {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if big == nil || big == small {
		t.Fatal("the 512 tier thumbnail never landed")
	}
	if w, _ := big.Size(); w != 300 {
		t.Fatalf("512 tier thumb = %d px wide, want the picture's own 300", w)
	}
}

// TestThumbCacheBudget covers the memory bound: entries are dropped
// oldest-first once the cache passes its byte budget or its entry count,
// and the running total follows them.
func TestThumbCacheBudget(t *testing.T) {
	clearThumbs()
	defer clearThumbs()
	pixel := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 1, 1)))

	// A megabyte an entry: the byte budget, not the count, is what bites.
	thumbMu.Lock()
	for i := 0; i < 200; i++ {
		key := thumbCacheKey("big:" + strconv.Itoa(i))
		thumbCache[key] = thumbEntry{bm: pixel, side: 512, bytes: 1 << 20}
		thumbOrder = append(thumbOrder, key)
		thumbBytes += 1 << 20
		trimThumbs()
	}
	overBudget, total := thumbBytes > thumbBudget, thumbBytes
	held, entries := len(thumbCache), len(thumbOrder)
	thumbMu.Unlock()
	if overBudget {
		t.Fatalf("cache holds %d bytes, over the %d budget", total, thumbBudget)
	}
	if held != entries {
		t.Fatalf("%d entries but %d in the eviction order", held, entries)
	}
	if held == 0 || held >= 200 {
		t.Fatalf("cache holds %d of 200 entries", held)
	}
	// The oldest went first: the newest key is still there.
	thumbMu.Lock()
	_, newestKept := thumbCache["big:199"]
	_, oldestKept := thumbCache["big:0"]
	thumbMu.Unlock()
	if !newestKept || oldestKept {
		t.Fatalf("newest kept = %v, oldest kept = %v", newestKept, oldestKept)
	}

	// Misses count against the entry cap, so a tree of folders with no
	// media cannot fill the map.
	clearThumbs()
	thumbMu.Lock()
	for i := 0; i < thumbMaxEntries+50; i++ {
		key := thumbCacheKey("miss:" + strconv.Itoa(i))
		thumbCache[key] = thumbEntry{side: 64}
		thumbOrder = append(thumbOrder, key)
		trimThumbs()
	}
	misses := len(thumbCache)
	thumbMu.Unlock()
	if misses > thumbMaxEntries {
		t.Fatalf("cache holds %d misses, over the %d cap", misses, thumbMaxEntries)
	}
}

func TestFolderCoverThumbLifecycle(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-fc")
	defer os.RemoveAll(dir)
	solidPNG(t, filepath.Join(dir, "a.png"), 8, color.RGBA{255, 0, 0, 255})
	solidPNG(t, filepath.Join(dir, "b.png"), 8, color.RGBA{0, 0, 255, 255})

	if bm := folderCoverThumb(dir, 96); bm != nil {
		t.Fatal("cache should start empty")
	}
	for i := 0; i < 200; i++ {
		if bm := folderCoverThumb(dir, 96); bm != nil {
			if w, h := bm.Size(); w != 128 || h != 128 {
				t.Fatalf("cover = %dx%d, want the 128 tier", w, h)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("folder cover never landed")
}

func TestFolderCoverOfPlainFolder(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-fcplain")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	key := thumbCacheKey("d:" + dir)
	if bm := folderCoverThumb(dir, 96); bm != nil {
		t.Fatal("a folder of text has no cover")
	}
	// The miss is remembered, so a folder with no media is not scanned per
	// frame; the second ask already knows there is nothing.
	for i := 0; i < 200; i++ {
		thumbMu.Lock()
		e, ok := thumbCache[key]
		thumbMu.Unlock()
		if ok {
			if e.bm != nil {
				t.Fatal("a folder of text should not grow a cover")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the empty cover was never recorded")
}

func TestClearThumbs(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-ct")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "pic.png")
	solidPNG(t, path, 8, color.RGBA{0, 255, 0, 255})

	mediaThumb(path, 96)
	var landed bool
	for i := 0; i < 200 && !landed; i++ {
		landed = mediaThumb(path, 96) != nil
		time.Sleep(20 * time.Millisecond)
	}
	if !landed {
		t.Fatal("thumbnail never landed")
	}

	// 刷新 forgets everything, so the next ask decodes again rather than
	// showing a picture whose file may have changed.
	clearThumbs()
	thumbMu.Lock()
	n := len(thumbCache)
	thumbMu.Unlock()
	if n != 0 {
		t.Fatalf("cache holds %d entries after a refresh", n)
	}
	if bm := mediaThumb(path, 96); bm != nil {
		t.Fatal("the cache should be empty again")
	}
}

func TestIconViewZoom(t *testing.T) {
	a := &App{mode: ViewExtraLarge, iconZoom: 1}
	if got := a.iconBoxSize(a.mode); got != 96 {
		t.Fatalf("extra large box = %v, want 96", got)
	}
	// The largest stop is 4×, so 超大图标 reaches 384 px.
	for i := 0; i < len(iconZoomSteps); i++ {
		a.zoomIn()
	}
	if a.iconZoom != 4 {
		t.Fatalf("iconZoom = %v, want the top stop", a.iconZoom)
	}
	if got := a.iconBoxSize(ViewExtraLarge); got != 384 {
		t.Fatalf("zoomed extra large box = %v, want 384", got)
	}
	if a.canZoomIn() {
		t.Fatal("nothing is bigger than the top stop")
	}
	for i := 0; i < len(iconZoomSteps); i++ {
		a.zoomOut()
	}
	if a.iconZoom != 1 || a.canZoomOut() {
		t.Fatalf("iconZoom = %v, want back at 1", a.iconZoom)
	}

	// From the row-based views the zoom opens an icon view instead.
	a.mode = ViewDetails
	a.zoomIn()
	if a.mode != ViewMediumIcons || a.iconZoom != 1 {
		t.Fatalf("zoomIn from 详细信息 = %v/%v", a.mode, a.iconZoom)
	}
	a.mode = ViewList
	a.zoomOut()
	if a.mode != ViewSmallIcons {
		t.Fatalf("zoomOut from 列表 = %v", a.mode)
	}
	if got := iconBaseSize(ViewExtraLarge); got != 96 {
		t.Fatalf("base size changed: %v", got)
	}
	if a.iconBoxSize(ViewDetails) != 32 || a.iconBoxSize(ViewList) != 32 {
		t.Fatal("the row-based views keep their 32 px icons")
	}
}

func TestTileFont(t *testing.T) {
	if f := tileFont(96); f != 12 {
		t.Fatalf("tileFont(96) = %v", f)
	}
	if f := tileFont(384); f != 18 {
		t.Fatalf("tileFont(384) = %v, want the 18 pt cap", f)
	}
	if f := tileFont(16); f != 12 {
		t.Fatalf("tileFont(16) = %v, want the 12 pt floor", f)
	}
}
