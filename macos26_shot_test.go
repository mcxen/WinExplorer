package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMacOS26Shots writes docs/assets/shots/MACOS26-V2 when SUNVALE_SHOT_DIR
// is set (demo iteration: before + light/dark files + assistant + settings).
func TestMacOS26Shots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	t.Cleanup(func() { visualV2 = false; testDark = nil; resolveTokens() })
	shot := func(dark bool, style, name string, setup func(a *App)) {
		d := dark
		testDark = &d
		resolveTokens()
		a, tt := tasksViewFixture(t)
		for _, n := range []string{"设计稿", "照片", "项目", "Archive"} {
			_ = os.Mkdir(filepath.Join(a.cur().path, n), 0o755)
		}
		samples := []struct {
			name string
			size int
		}{
			{"季度报告.pdf", 40 << 10},
			{"预算-2026.xlsx", 28 << 10},
			{"封面.png", 120 << 10},
			{"演示.pptx", 64 << 10},
			{"Meeting Notes.md", 8 << 10},
			{"project-src.tar.gz", 256 << 10},
			{"旅行照片.jpg", 90 << 10},
		}
		for _, s := range samples {
			_ = os.WriteFile(filepath.Join(a.cur().path, s.name), make([]byte, s.size), 0o644)
		}
		a.reload()
		a.applySettings(func(x *appSettings) { x.VisualStyle = style })
		tk := startArchiveTask("extract", "/home/u/Downloads/照片备份-2026.zip", "user", 812<<20)
		tk.resetItems(812<<20, 3872)
		tk.add(300 << 20)
		defer tk.finish(nil)
		a.selectOnly(filepath.Join(a.cur().path, "季度报告.pdf"))
		tt.Frame()
		setup(a)
		for i := 0; i < 3; i++ {
			tt.Frame()
			tt.Advance(flipDuration + 100*time.Millisecond)
		}
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_ = png.Encode(f, tt.Image())
	}
	none := func(*App) {}
	// 01-before: classic (main default look) for side-by-side review.
	shot(false, "", "01-before.png", none)
	shot(false, "v2", "02-files-light.png", none)
	shot(true, "v2", "03-files-dark.png", none)
	shot(false, "v2", "04-assistant.png", func(a *App) { a.assistantOpen = true })
	shot(false, "v2", "05-settings.png", func(a *App) { a.settingsOpen = true })
}
