package main

import (
	"path/filepath"
	"testing"
	"time"
)

// TestAIFxRowsInWindow runs the feedback in a laid-out window: a row the
// assistant works on lights up toward its right edge, and once moved away
// it slides out and leaves the listing before any reload.
func TestAIFxRowsInWindow(t *testing.T) {
	a, tt, dir := keynavWindow(t, 3)
	saved := aiFx
	aiFx = newAIFx()
	aiFx.now = tt.Now
	t.Cleanup(func() { aiFx = saved })

	target := filepath.Join(dir, "f001.txt")
	r, ok := tt.Find("f001.txt")
	if !ok {
		t.Fatalf("row not shown: %v", tt.Texts())
	}
	y := int(r.Y + r.H/2)
	img := tt.Image()
	right := img.Bounds().Dx() - 40
	_, _, b0, _ := img.At(right, y).RGBA()

	aiFx.markBusy(target)
	tt.Frame()                // the app's repaint() is a no-op under the tester
	tt.Advance(aiFxPulse / 2) // the breath at its fullest
	img = tt.Image()
	rr, gg, bb, _ := img.At(right, y).RGBA()
	if !(bb < b0 || rr < bb) || rr>>8 > 0xE0 {
		t.Fatalf("no accent glow at the row's right edge: rgb %d %d %d", rr>>8, gg>>8, bb>>8)
	}
	lr, _, lb, _ := img.At(int(r.X)+4, y).RGBA()
	if lr>>8 < 0xF0 || lb>>8 < 0xF0 {
		t.Fatalf("the glow should leave the row's left side clear: %d %d", lr>>8, lb>>8)
	}

	aiFx.markLeaving(target)
	tt.Frame()
	for d := time.Duration(0); d < aiFxMinGlow+aiFxSlide+aiFxFold+100*time.Millisecond; d += 40 * time.Millisecond {
		tt.Advance(40 * time.Millisecond)
	}
	if tt.HasText("f001.txt") {
		t.Fatal("the moved row is still listed after its slide")
	}
	if !tt.HasText("f000.txt") || !tt.HasText("f002.txt") {
		t.Fatalf("the other rows should stay: %v", tt.Texts())
	}
	if len(a.visibleEntries()) != 2 {
		t.Fatalf("visibleEntries = %d, want 2", len(a.visibleEntries()))
	}
}
