package main

import (
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// aifx.go is the file pane's feedback while the assistant works on files.
// The assistant's tools run on its own goroutine and touch the disk long
// before the listing reloads, so the rows they are about to change glow
// (a soft light breathing on their right side), a renamed item's name
// flashes the accent once the listing shows it, and an item moved or
// trashed out of the folder slides out to the left and folds its row shut
// before the reload drops it. The store is keyed by clean absolute path and
// guarded by its own lock: the tools write it, the frame reads it.

const (
	// aiFxMinGlow is how long a row glows before it may slide away: the
	// file calls finish in microseconds, and a row that vanishes the frame
	// it lit up says nothing.
	aiFxMinGlow = 900 * time.Millisecond
	aiFxSlide   = 340 * time.Millisecond
	aiFxFold    = 320 * time.Millisecond
	aiFxFlash   = 1200 * time.Millisecond
	// aiFxPulse is one breath of the glow.
	aiFxPulse = 1600 * time.Millisecond
)

type aiFxStore struct {
	mu      sync.Mutex
	busy    map[string]time.Time
	leaving map[string]time.Time
	// renamed holds the time the new name was first drawn; zero until then,
	// as the listing may reload long after the rename itself.
	renamed map[string]time.Time
	now     func() time.Time
}

var aiFx = newAIFx()

func newAIFx() *aiFxStore {
	return &aiFxStore{
		busy:    map[string]time.Time{},
		leaving: map[string]time.Time{},
		renamed: map[string]time.Time{},
		now:     time.Now,
	}
}

func aiFxKey(p string) string { return filepath.Clean(p) }

// markBusy lights the rows of paths. A path already glowing keeps the time
// it lit up, so the breath does not restart.
func (s *aiFxStore) markBusy(paths ...string) {
	s.mu.Lock()
	now := s.now()
	for _, p := range paths {
		if p == "" {
			continue
		}
		k := aiFxKey(p)
		if _, ok := s.busy[k]; !ok {
			s.busy[k] = now
		}
	}
	s.mu.Unlock()
	repaint()
}

// clearBusy puts the light out on paths whose operation failed.
func (s *aiFxStore) clearBusy(paths ...string) {
	s.mu.Lock()
	for _, p := range paths {
		delete(s.busy, aiFxKey(p))
	}
	s.mu.Unlock()
	repaint()
}

// markLeaving starts path's slide out, no sooner than aiFxMinGlow after it
// lit up.
func (s *aiFxStore) markLeaving(path string) {
	s.mu.Lock()
	k := aiFxKey(path)
	at := s.now()
	if lit, ok := s.busy[k]; ok && lit.Add(aiFxMinGlow).After(at) {
		at = lit.Add(aiFxMinGlow)
	}
	s.leaving[k] = at
	s.mu.Unlock()
	repaint()
}

// markRenamed moves the light from the old name to the new one, which
// flashes once the listing shows it.
func (s *aiFxStore) markRenamed(from, to string) {
	s.mu.Lock()
	// The old row keeps glowing until the reload swaps it for the new one,
	// so only the new name is noted.
	_ = from
	s.renamed[aiFxKey(to)] = time.Time{}
	s.mu.Unlock()
	repaint()
}

// endRun puts every light out when the assistant's run ends; the slides and
// flashes play on.
func (s *aiFxStore) endRun() {
	s.mu.Lock()
	s.busy = map[string]time.Time{}
	s.mu.Unlock()
	repaint()
}

// aiFxLook is what a row draws this frame.
type aiFxLook struct {
	// glow is the breath's strength, 0 when the row is not glowing.
	glow float32
	// slide runs 0 to 1 as the row slides out; fold 0 to 1 as it then
	// closes. gone is a row whose fold is over: the view leaves it out.
	slide, fold float32
	gone        bool
	// flash fades 1 to 0 over a fresh name.
	flash float32
	// live is a look still changing, which needs another frame; moving is
	// one whose layout changes (a slide, a fold, a flash, a slide about to
	// start), which needs the view built again rather than only painted.
	live, moving bool
}

func (l aiFxLook) any() bool { return l.live || l.gone }

// look reports path's state at the store's clock.
func (s *aiFxStore) look(path string) aiFxLook {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.busy)+len(s.leaving)+len(s.renamed) == 0 {
		return aiFxLook{}
	}
	now := s.now()
	k := aiFxKey(path)
	var l aiFxLook
	if at, ok := s.leaving[k]; ok {
		d := now.Sub(at)
		switch {
		case d < 0:
			// Still in its minimum glow.
		case d < aiFxSlide:
			l.slide = easeIn(float32(d) / float32(aiFxSlide))
		case d < aiFxSlide+aiFxFold:
			l.slide = 1
			l.fold = easeOut(float32(d-aiFxSlide) / float32(aiFxFold))
		default:
			return aiFxLook{gone: true}
		}
		l.live, l.moving = true, true
	}
	if lit, ok := s.busy[k]; ok && l.slide == 0 {
		l.glow = breath(now.Sub(lit))
		l.live = true
	}
	if at, ok := s.renamed[k]; ok {
		if at.IsZero() {
			at = now
			s.renamed[k] = at
		}
		if d := now.Sub(at); d < aiFxFlash {
			l.flash = 1 - float32(d)/float32(aiFxFlash)
			l.live, l.moving = true, true
		} else {
			delete(s.renamed, k)
		}
	}
	return l
}

// pendingLeave is how long the slowest slide still has to play: the reload
// that drops the rows waits for it.
func (s *aiFxStore) pendingLeave() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var most time.Duration
	for _, at := range s.leaving {
		if d := at.Add(aiFxSlide + aiFxFold).Sub(now); d > most {
			most = d
		}
	}
	return most
}

// forgetLeft drops the slides that are over, once the reload has taken
// their rows out of the listing.
func (s *aiFxStore) forgetLeft() {
	s.mu.Lock()
	now := s.now()
	for k, at := range s.leaving {
		if now.Sub(at) >= aiFxSlide+aiFxFold {
			delete(s.leaving, k)
		}
	}
	s.mu.Unlock()
}

// breath is the glow's strength d into it: it fades in over the first
// 200 ms, then swells between 0.35 and 1 once per aiFxPulse.
func breath(d time.Duration) float32 {
	ph := float64(d%aiFxPulse) / float64(aiFxPulse)
	v := float32(0.675 - 0.325*math.Cos(2*math.Pi*ph))
	if d < 200*time.Millisecond {
		v *= float32(d) / float32(200*time.Millisecond)
	}
	return v
}

func easeIn(t float32) float32  { return t * t }
func easeOut(t float32) float32 { return 1 - (1-t)*(1-t) }

// aiFxRow applies path's look to its row or tile: the slide and fade, the
// fold of its height (full is its resting height, 0 for a tile that keeps
// its box), and the glow under its children. The glow breathes at paint
// time, so a frame that only repaints keeps it moving; a slide, fold or
// flash changes the layout and asks for the view to be built again. The
// desktop's reduce-motion preference leaves only the steady states: a
// still glow, and a row that is gone once its time is up.
func aiFxRow(c *ui.Context, el ui.Element, path string, full, radius float32) aiFxLook {
	l := aiFx.look(path)
	if !l.any() {
		return l
	}
	calm := c.Preferences().ReduceMotion
	if l.moving {
		c.After(16 * time.Millisecond)
	}
	if l.slide > 0 && !calm {
		w := el.Bounds().W
		if w <= 0 {
			w = 240
		}
		el.Translate(-w*1.05*l.slide, 0).Opacity(1 - l.slide)
		if full > 0 {
			el.Height(full * (1 - l.fold)).Clip()
		}
	}
	if l.glow > 0 || l.live {
		el.Draw(func(p *ui.Painter, r ui.Rect) {
			now := aiFx.look(path)
			glow := now.glow
			if calm && glow > 0 {
				glow = 0.8
			}
			if glow > 0 {
				paintAIGlow(p, r, glow, radius)
			}
			if now.live && !calm {
				p.AnimationFrame()
			}
		})
	}
	return l
}

// paintAIGlow is the breathing light: clear on the left, gathering into
// the accent toward the row's right edge.
func paintAIGlow(p *ui.Painter, r ui.Rect, strength, radius float32) {
	acc := colorAccent()
	tip := ui.Hex("#4CC2FF")
	if darkMode() {
		tip = acc
	}
	// The left 30 % stays clear; the gradient builds over the rest.
	start := r.W * 0.3
	band := ui.Rect{X: r.X + start, Y: r.Y, W: r.W - start, H: r.H}
	p.FillGradient(band, ui.LinearGradient{
		From:  acc.Alpha(0),
		To:    tip.Alpha(0.42 * strength),
		Angle: 90,
	}, radius)
}

// flashColor tints a name with the accent while it is fresh.
func flashColor(base ui.Color, l aiFxLook) ui.Color {
	if l.flash <= 0 {
		return base
	}
	return base.Mix(colorAccent(), l.flash)
}
