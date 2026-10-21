package main

import (
	"sync"
	"testing"
	"time"
)

// fakeClock drives an aiFxStore through its timeline.
type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time      { return f.t }
func (f *fakeClock) add(d time.Duration) { f.t = f.t.Add(d) }
func newTestFx() (*aiFxStore, *fakeClock) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	s := newAIFx()
	s.now = clk.now
	return s, clk
}

func TestAIFxLeaveTimeline(t *testing.T) {
	s, clk := newTestFx()
	s.markBusy("/d/a.txt")
	clk.add(100 * time.Millisecond)
	if l := s.look("/d/a.txt"); l.glow <= 0 || !l.live {
		t.Fatalf("busy row should glow: %+v", l)
	}
	if l := s.look("/d/b.txt"); l.any() {
		t.Fatalf("untouched row has a look: %+v", l)
	}
	// Moved away 100 ms after lighting up: the slide waits out the glow.
	s.markLeaving("/d/./a.txt")
	clk.add(aiFxMinGlow - 200*time.Millisecond)
	if l := s.look("/d/a.txt"); l.slide != 0 || l.glow <= 0 {
		t.Fatalf("slide began inside the minimum glow: %+v", l)
	}
	if d := s.pendingLeave(); d != 100*time.Millisecond+aiFxSlide+aiFxFold {
		t.Fatalf("pendingLeave = %v", d)
	}
	clk.add(100*time.Millisecond + aiFxSlide/2)
	l := s.look("/d/a.txt")
	if l.slide <= 0 || l.slide >= 1 || l.fold != 0 || l.glow != 0 {
		t.Fatalf("mid slide: %+v", l)
	}
	clk.add(aiFxSlide/2 + aiFxFold/2)
	if l := s.look("/d/a.txt"); l.slide != 1 || l.fold <= 0 || l.fold >= 1 {
		t.Fatalf("mid fold: %+v", l)
	}
	s.forgetLeft() // a reload mid-fold keeps the slide going
	clk.add(aiFxFold)
	if l := s.look("/d/a.txt"); !l.gone {
		t.Fatalf("row should be gone: %+v", l)
	}
	if s.pendingLeave() != 0 {
		t.Fatal("finished slide still pending")
	}
	s.forgetLeft()
	if l := s.look("/d/a.txt"); l.gone {
		t.Fatal("a path that comes back (undo) must show again")
	}
}

func TestAIFxRenameFlashAndRunEnd(t *testing.T) {
	s, clk := newTestFx()
	s.markBusy("/d/old.jpg", "/d/other.jpg")
	s.markRenamed("/d/old.jpg", "/d/new.jpg")
	// The flash waits for the listing to show the new name.
	clk.add(5 * time.Second)
	if l := s.look("/d/new.jpg"); l.flash != 1 {
		t.Fatalf("first sight should flash fully: %+v", l)
	}
	clk.add(aiFxFlash / 2)
	if l := s.look("/d/new.jpg"); l.flash <= 0 || l.flash >= 1 {
		t.Fatalf("flash should be fading: %+v", l)
	}
	s.clearBusy("/d/other.jpg")
	if s.look("/d/other.jpg").glow != 0 {
		t.Fatal("cleared row still glows")
	}
	// old.jpg has already glowed past the minimum; endRun puts it out.
	s.endRun()
	if s.look("/d/old.jpg").glow != 0 {
		t.Fatal("run end should put a fully glowed light out")
	}
	clk.add(aiFxFlash)
	if l := s.look("/d/new.jpg"); l.any() {
		t.Fatalf("flash should be over: %+v", l)
	}
}

func TestAIFxEndRunKeepsMinimumGlow(t *testing.T) {
	s, clk := newTestFx()
	s.markBusy("/d/fast.txt")
	clk.add(100 * time.Millisecond)
	s.endRun()
	if l := s.look("/d/fast.txt"); l.glow <= 0 || !l.live {
		t.Fatalf("fast run should keep glowing through the minimum: %+v", l)
	}
	clk.add(aiFxMinGlow - 100*time.Millisecond)
	if l := s.look("/d/fast.txt"); l.glow != 0 {
		t.Fatalf("glow should end once the minimum is reached: %+v", l)
	}
}

func TestAIFxBreathRange(t *testing.T) {
	for d := 200 * time.Millisecond; d < 2*aiFxPulse; d += 50 * time.Millisecond {
		if v := breath(d); v < 0.34 || v > 1.001 {
			t.Fatalf("breath(%v) = %v", d, v)
		}
	}
	if breath(0) != 0 {
		t.Fatal("glow should fade in from nothing")
	}
}

func TestAIFxGlowBandFitsRow(t *testing.T) {
	// A narrow tile still gets a band that stays inside its box.
	band := aiFxGlowBand
	inset := aiFxGlowInset
	if band+inset > 80 {
		t.Fatalf("glow band+inset %v too wide for a small icon tile", band+inset)
	}
	if aiFxGlowAlpha <= 0.42 || aiFxGlowAlpha > 1 {
		t.Fatalf("glow tip alpha %v out of the intended range", aiFxGlowAlpha)
	}
}

// The assistant's goroutine writes while the frame reads.
func TestAIFxConcurrent(t *testing.T) {
	s := newAIFx()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			s.markBusy("/d/x")
			s.markLeaving("/d/x")
			s.markRenamed("/d/x", "/d/y")
			s.endRun()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			s.look("/d/x")
			s.look("/d/y")
			s.pendingLeave()
			s.forgetLeft()
		}
	}()
	wg.Wait()
}
