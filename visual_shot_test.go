package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestVisualStyleSwitch: 视觉风格 v2 re-resolves the tokens and back.
func TestVisualStyleSwitch(t *testing.T) {
	a, tt := tasksViewFixture(t)
	t.Cleanup(func() { visualV2 = false; resolveTokens() })
	// Fresh installs default to v2; pin classic first so the switch is visible.
	a.applySettings(func(x *appSettings) { x.VisualStyle = "" })
	tt.Frame()
	classic := colorAccent()
	if visualV2 {
		t.Fatal("classic not applied")
	}
	a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
	tt.Frame()
	if !visualV2 || colorAccent() == classic {
		t.Fatal("v2 tokens not applied")
	}
	a.applySettings(func(x *appSettings) { x.VisualStyle = "" })
	tt.Frame()
	if visualV2 || colorAccent() != classic {
		t.Fatal("classic not restored")
	}
}

// TestVisualShots writes VISUAL2 before/after, light and dark, when
// SUNVALE_SHOT_DIR is set.
func TestVisualShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	t.Cleanup(func() { visualV2 = false; testDark = nil; resolveTokens() })
	for _, dark := range []bool{false, true} {
		for _, style := range []string{"", "v2"} {
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
			a.applySettings(func(x *appSettings) { x.VisualStyle = style })
			tk := startArchiveTask("extract", "/home/u/Downloads/照片备份-2026.zip", "user", 812<<20)
			tk.resetItems(812<<20, 3872)
			tk.setItem("2026/09/IMG_20260918_182955.HEIC", 3<<20)
			tk.add(300 << 20)
			a.selectOnly(filepath.Join(a.cur().path, "季度报告.pdf"))
			name := map[string]string{"": "classic", "v2": "v2"}[style] + map[bool]string{false: "-light", true: "-dark"}[dark]
			save := func(n string) {
				f, err := os.Create(filepath.Join(dir, n))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				_ = png.Encode(f, tt.Image())
			}
			tt.Frame()
			tt.Frame()
			save("files-" + name + ".png")
			a.tasksFlipped = true
			tt.Frame()
			tt.Advance(flipDuration + 100*time.Millisecond)
			tt.Frame()
			tt.Frame()
			save("tasks-" + name + ".png")
			tk.finish(nil)
			_ = ui.Center
		}
	}
}
