package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestArchiveProgressShots writes the ARCHIVE-PROG screenshots when
// SUNVALE_SHOT_DIR is set.
func TestArchiveProgressShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	isolateArchiveTasks(t)
	now := time.Now()
	old := archiveNow
	archiveNow = func() time.Time { return now }
	t.Cleanup(func() { archiveNow = old })
	save := func(name string, tt *ui.Tester) {
		for range 4 {
			tt.Frame()
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, t.TempDir())
	at := now.Add(-time.Minute)
	a.assist.turns = []aiTurn{{Role: "user", Text: "把下载里的两个压缩包解开，再把报告文件夹打包", At: at}, {Role: "assistant", Text: "正在处理。", At: at}}
	a.assist.running = true
	a.assist.status = "正在解压"
	x := startArchiveTask("extract", "/home/u/Downloads/照片备份-2026.zip", "ai", 812<<20)
	x.add(342 << 20)
	x.setFile("2026/09/IMG_20260918_183021.HEIC")
	y := startArchiveTask("extract", "/home/u/Downloads/project-src.tar.gz", "ai", 96<<20)
	z := startArchiveTask("compress", "/home/u/Documents/季度报告.zip", "ai", 48<<20)
	now = now.Add(40 * time.Second)
	y.add(31 << 20)
	y.setFile("src/internal/fsops/copy.go")
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(appTheme(c))
		ui.Column(c).Fill().Background(colorSurface()).Children(func() { a.assistantTranscript(c) })
	}, 520, 520)
	save("01-assistant-live.png", tt)
	x.add(470 << 20)
	x.finish(nil)
	y.finish(errors.New("磁盘空间不足：还需 65 MB"))
	z.cancel()
	z.finish(errArchiveCanceled)
	a.assist.running = false
	save("02-assistant-done-error.png", tt)
	b := newTestApp(t, t.TempDir())
	startArchiveTask("extract", "/home/u/Downloads/照片备份-2026.zip", "user", 812<<20).add(342 << 20)
	sb := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(appTheme(c))
		ui.Column(c).Fill().Background(colorSurface()).Justify(ui.End).Children(func() { b.statusBar(c) })
	}, 900, 40)
	save("03-status-bar.png", sb)
}
