package main

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAfinfoDuration(t *testing.T) {
	out := `File:           /System/Library/Sounds/Submarine.aiff
File type ID:   AIFF
Num Tracks:     1
----
Data format:     2 ch,  44100 Hz, 'lpcm' (0x0000000C) 24-bit little-endian signed integer
                no channel layout.
estimated duration: 1.492 sec
audio bytes: 394056
audio bytes encrypted: no
`
	d, ok := parseAfinfoDuration(out)
	if !ok || d != 1492*time.Millisecond {
		t.Fatalf("parse = %v %v", d, ok)
	}
	if _, ok := parseAfinfoDuration("no duration here"); ok {
		t.Fatal("missing line must not parse")
	}
	// Zero and negative durations are rejected.
	if _, ok := parseAfinfoDuration("estimated duration: 0.000 sec"); ok {
		t.Fatal("zero duration accepted")
	}
}

func TestAfplayFallback(t *testing.T) {
	if _, err := os.Stat("/usr/bin/afplay"); err != nil {
		t.Skip("afplay not on this machine")
	}
	// .aiff is beyond MyGo's WAV/MP3/Vorbis decoders: the fallback plays
	// it through afplay, with afinfo's duration.
	const aiff = "/System/Library/Sounds/Submarine.aiff"
	if _, err := os.Stat(aiff); err != nil {
		t.Skip("no system sound to test with")
	}
	mi := loadAudio(aiff)
	if mi.player == nil {
		t.Fatal("afplay fallback did not engage for aiff")
	}
	if mi.player.player != nil {
		t.Fatal("should be in afplay mode, not MyGo's player")
	}
	if mi.player.dur <= 0 {
		t.Fatalf("duration = %v", mi.player.dur)
	}
	if mi.player.playing() {
		t.Fatal("not started yet")
	}
	// Toggle twice: start, then stop; a second toggle must not double-run.
	mi.player.toggle()
	if !mi.player.playing() {
		t.Fatal("afplay did not start")
	}
	mi.player.toggle()
	if mi.player.playing() {
		t.Fatal("afplay did not stop")
	}
	mi.player.stop()
	if mi.player.position() != 0 && mi.player.position() != mi.player.dur {
		t.Fatalf("position after stop = %v", mi.player.position())
	}
}

func TestQuickLookStep(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-qlstep")
	defer os.RemoveAll(dir)
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	a := newTestApp(t, dir)
	b := filepath.Join(dir, "b.txt")
	a.selectOnly(b)

	a.quickLookStep(1)
	if sel := a.singleSelected(); sel.Path != filepath.Join(dir, "c.txt") {
		t.Fatalf("step +1 = %v", sel.Path)
	}
	a.quickLookStep(1)
	if sel := a.singleSelected(); sel.Path != filepath.Join(dir, "c.txt") {
		t.Fatalf("past the end must hold: %v", sel.Path)
	}
	a.quickLookStep(-1)
	if sel := a.singleSelected(); sel.Path != b {
		t.Fatalf("step -1 = %v", sel.Path)
	}
	a.quickLookStep(-1)
	a.quickLookStep(-1)
	if sel := a.singleSelected(); sel.Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("two steps back = %v", sel.Path)
	}
	a.quickLookStep(-1)
	if sel := a.singleSelected(); sel.Path != filepath.Join(dir, "a.txt") {
		t.Fatalf("before the start must hold: %v", sel.Path)
	}
}

// Launching costs neither registry scan: both start lazily, on the first
// 打开方式 menu and the first gallery visit.
func TestLazyStartupScans(t *testing.T) {
	a := newTestApp(t, "/tmp")
	_ = a
	if galleryScan.Load() != nil {
		t.Fatal("gallery scan ran at launch")
	}
	if openWith.Load() != nil {
		t.Fatal("open-with scan ran at launch")
	}
	// The first menu request starts the registry scan exactly once.
	ensureOpenWithScan()
	ensureOpenWithScan()
	if !ensureOpenWithScanOnce.Load() {
		t.Fatal("scan flag not set")
	}
}

func TestGifPlayer(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-gif")
	defer os.RemoveAll(dir)
	// A two-frame GIF encoded with the stdlib.
	path := filepath.Join(dir, "anim.gif")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 8, 8), color.Palette{color.White, color.Black})
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			pal.SetColorIndex(x, y, uint8((x+y)%2))
		}
	}
	g := &gif.GIF{
		Image:  []*image.Paletted{pal, pal},
		Delay:  []int{5, 5},
		Config: image.Config{Width: 8, Height: 8},
	}
	if err := gif.EncodeAll(f, g); err != nil {
		t.Fatal(err)
	}
	f.Close()

	pl := newGifPlayer(path)
	if pl == nil {
		t.Fatal("player not built for a 2-frame gif")
	}
	if len(pl.frames) != 2 || pl.delays[0] != 50*time.Millisecond {
		t.Fatalf("frames = %d delays = %v", len(pl.frames), pl.delays)
	}
	if w, h := pl.bm.Size(); w != 8 || h != 8 {
		t.Fatalf("canvas = %dx%d", w, h)
	}
	// A still gif gets no player.
	still := filepath.Join(dir, "still.gif")
	fs, _ := os.Create(still)
	gif.Encode(fs, pal, nil)
	fs.Close()
	if pl := newGifPlayer(still); pl != nil {
		t.Fatal("still gif should not animate")
	}
	// Missing and malformed files come back nil.
	if pl := newGifPlayer(filepath.Join(dir, "nope.gif")); pl != nil {
		t.Fatal("missing file built a player")
	}
	txt := filepath.Join(dir, "bad.gif")
	os.WriteFile(txt, []byte("not a gif"), 0o644)
	if pl := newGifPlayer(txt); pl != nil {
		t.Fatal("garbage built a player")
	}
}
