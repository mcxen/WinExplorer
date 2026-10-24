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

// TestProgressStyleShots writes PROG-STYLE screenshots when
// SUNVALE_SHOT_DIR is set: the same five states as bars, then as gauges.
func TestProgressStyleShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	isolateArchiveTasks(t)
	now := time.Now()
	old := archiveNow
	archiveNow = func() time.Time { return now }
	t.Cleanup(func() { archiveNow = old })
	run := startArchiveTask("extract", "/u/照片备份-2026.zip", "ai", 812<<20)
	run.setFile("2026/09/IMG_20260918_183021.HEIC")
	ext := startArchiveTask("extract", "/u/资料.7z", "ai", 0)
	done := startArchiveTask("compress", "/u/季度报告.zip", "ai", 48<<20)
	bad := startArchiveTask("extract", "/u/project-src.tar.gz", "ai", 96<<20)
	can := startArchiveTask("compress", "/u/视频素材.zip", "ai", 3<<30)
	now = now.Add(40 * time.Second)
	run.add(342 << 20)
	done.finish(nil)
	bad.add(31 << 20)
	bad.finish(errors.New("磁盘空间不足：还需 65 MB"))
	can.add(1 << 30)
	can.cancel()
	can.finish(errArchiveCanceled)
	snaps := archiveSnaps(nil)
	_ = ext
	for _, shot := range []struct {
		name  string
		gauge bool
		h     int
	}{{"01-bar.png", false, 350}, {"02-gauge.png", true, 520}} {
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(appTheme(c))
			ui.Column(c).Fill().Padding(10, 10).Gap(8).Background(colorSurface()).Children(func() {
				for _, s := range snaps {
					cc := c.Key(s.key())
					if shot.gauge {
						paintArchiveGaugeCard(cc, s)
					} else {
						paintArchiveTaskCard(cc, s)
					}
				}
			})
		}, 520, shot.h)
		for range 4 {
			tt.Frame()
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
