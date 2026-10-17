package main

import (
	"image"
	"image/gif"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// gifplay.go animates GIFs in the quick preview. DecodeBitmap shows the
// first frame only; this composites the frames onto one RGBA canvas and
// uploads it with Bitmap.Update — the interface MyGo keeps for video
// frames — on the main thread, at each frame's own delay, until the
// preview closes or moves on.

const (
	gifMaxFrames   = 300  // play the first N frames
	gifMaxSide     = 1024 // px; larger gifs scale down before playing
	gifDefaultTick = 100 * time.Millisecond
)

type gifPlayer struct {
	mu     sync.Mutex
	bm     *ui.Bitmap
	frames []*image.Paletted
	delays []time.Duration
	bounds image.Rectangle // the composite canvas
	idx    int
	stop   chan struct{}
	closed bool
}

var (
	gifMu   sync.Mutex
	gifPlay = map[string]*gifPlayer{}
)

// gifFor returns a playing player for path, starting one when none runs;
// players for other paths are stopped, since one preview shows at a time.
// The GIF plugin owns the animation: switched off, gifFor returns nil, and
// the callers keep the decoded still, as before the plugin existed.
func gifFor(path string) *ui.Bitmap {
	if !pluginOn(pluginStore.Load(), "gif") {
		return nil
	}
	gifMu.Lock()
	defer gifMu.Unlock()
	for p, pl := range gifPlay {
		if p != path {
			pl.shutdown()
			delete(gifPlay, p)
		}
	}
	if pl := gifPlay[path]; pl != nil {
		return pl.bitmap()
	}
	pl := newGifPlayer(path)
	if pl == nil {
		return nil
	}
	gifPlay[path] = pl
	go pl.play()
	return pl.bitmap()
}

func newGifPlayer(path string) *gifPlayer {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil
	}
	if len(g.Image) < 2 {
		return nil // a still gif needs no player
	}
	n := len(g.Image)
	if n > gifMaxFrames {
		n = gifMaxFrames
	}
	frames := g.Image[:n]
	delays := make([]time.Duration, n)
	for i := range n {
		d := 10 * time.Millisecond
		if g.Delay[i] > 0 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		delays[i] = d
	}
	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		b := frames[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	// Scale the canvas down when the gif is enormous, as a whole.
	if w > gifMaxSide || h > gifMaxSide {
		scale := float64(gifMaxSide) / float64(max(w, h))
		w, h = max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	}
	bounds := image.Rect(0, 0, w, h)
	return &gifPlayer{
		bm:     ui.NewBitmap(image.NewRGBA(bounds)),
		frames: frames,
		delays: delays,
		bounds: bounds,
		stop:   make(chan struct{}),
	}
}

func (p *gifPlayer) bitmap() *ui.Bitmap { return p.bm }

// compositeLocked draws frames[0..idx] onto a fresh canvas — a gif frame
// can carry only the pixels that changed, so the canvas keeps what
// earlier frames drew.
func (p *gifPlayer) compositeLocked() *image.RGBA {
	w, h := p.bm.Size()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i <= p.idx && i < len(p.frames); i++ {
		fr := p.frames[i]
		dr := fr.Bounds().Intersect(p.bounds)
		if dr.Empty() {
			continue
		}
		sr := dr.Sub(fr.Bounds().Min)
		xdraw.Draw(canvas, dr, fr, sr.Min, xdraw.Over)
	}
	return canvas
}

// play steps the frames at their delays. Bitmap.Update runs on the main
// thread, as it asks, with a repaint after each frame.
func (p *gifPlayer) play() {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		delay := p.delays[p.idx]
		canvas := p.compositeLocked()
		bm := p.bm
		p.mu.Unlock()
		mygo.RunOnMain(func() {
			bm.Update(canvas)
			repaint()
		})
		select {
		case <-p.stop:
			return
		case <-time.After(delay):
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		p.idx = (p.idx + 1) % len(p.frames)
		p.mu.Unlock()
	}
}

// shutdown ends the animation; the goroutine finishes on its next tick.
func (p *gifPlayer) shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	close(p.stop)
}

// stopGifPlay ends every animation, when the quick preview closes.
func stopGifPlay() {
	gifMu.Lock()
	defer gifMu.Unlock()
	for _, pl := range gifPlay {
		pl.shutdown()
	}
	gifPlay = map[string]*gifPlayer{}
}
