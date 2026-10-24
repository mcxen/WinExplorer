package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sunvale/internal/forms"
)

// TestOpenUIMoreShots writes the OPENUI-MORE2 screenshots when
// SUNVALE_SHOT_DIR is set: the demo Gallery at three columns, and the same
// card in a narrow pane where the grid falls back to auto fit.
func TestOpenUIMoreShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	for _, shot := range []struct {
		name string
		w, h int
	}{{"05-gallery-cols.png", 760, 880}, {"06-gallery-narrow.png", 300, 1000}} {
		a := newStoreApp(t, t.TempDir())
		a.assist.turns = []aiTurn{{Role: "user", Text: "openui-more"}, {Role: "assistant", Text: forms.FenceOpenUI(openUIMoreDemoProgram())}}
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(appTheme(c))
			ui.Column(c).Fill().Background(colorSurface()).Children(func() { a.assistantTranscript(c) })
		}, shot.w, shot.h)
		for range 20 {
			tt.Frame()
			time.Sleep(100 * time.Millisecond)
		}
		f, err := os.Create(filepath.Join(dir, shot.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
