package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMacOS26Shots writes docs/assets/shots/MACOS26 when SUNVALE_SHOT_DIR
// is set.
func TestMacOS26Shots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	t.Cleanup(func() { visualV2 = false; testDark = nil; resolveTokens() })
	shot := func(dark bool, name string, setup func(a *App)) {
		d := dark
		testDark = &d
		resolveTokens()
		a, tt := tasksViewFixture(t)
		for _, n := range []string{"设计稿", "照片", "项目"} {
			_ = os.Mkdir(filepath.Join(a.cur().path, n), 0o755)
		}
		for _, n := range []string{"季度报告.pdf", "预算-2026.xlsx", "封面.png", "演示.pptx"} {
			_ = os.WriteFile(filepath.Join(a.cur().path, n), make([]byte, 40<<10), 0o644)
		}
		a.reload()
		a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
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
	shot(false, "01-files-light.png", none)
	shot(true, "02-files-dark.png", none)
	shot(false, "03-assistant-light.png", func(a *App) { a.assistantOpen = true })
	shot(false, "04-tasks-flip.png", func(a *App) { a.tasksFlipped = true })
	shot(false, "05-settings.png", func(a *App) { a.settingsOpen = true })
	shot(false, "08-selection-focused.png", func(a *App) { a.navTree.focused = false })
	shot(false, "09-selection-unfocused.png", func(a *App) { a.navTree.focused = true })
	shot(true, "10-selection-focused-dark.png", func(a *App) { a.navTree.focused = false })
}
