package main

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/tasks"
)

func tasksViewFixture(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	isolateArchiveTasks(t)
	dir := t.TempDir()
	for _, n := range []string{"照片备份-2026.zip", "project-src.tar.gz", "会议纪要.md"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	return a, ui.NewTester(a.view, 1280, 800)
}

func TestTasksFlipShortcutAndEsc(t *testing.T) {
	a, tt := tasksViewFixture(t)
	tt.Frame()
	if !tt.HasText("会议纪要.md") {
		t.Fatalf("front: %v", tt.Texts())
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyJ)
	if !a.tasksFlipped {
		t.Fatal("Ctrl+Shift+J did not flip")
	}
	tt.Advance(flipDuration + 50*time.Millisecond)
	tt.Frame()
	if !tt.HasText("进行中 0") || tt.HasText("会议纪要.md") {
		t.Fatalf("back: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(flipDuration + 50*time.Millisecond)
	tt.Frame()
	if a.tasksFlipped || !tt.HasText("会议纪要.md") {
		t.Fatalf("Esc: %v", tt.Texts())
	}
	if err := tt.Click("任务 (Ctrl+Shift+J)"); err != nil || !a.tasksFlipped {
		t.Fatalf("toolbar: %v", err)
	}
}

func TestTasksViewShowsMemberAndControls(t *testing.T) {
	a, tt := tasksViewFixture(t)
	tk := startArchiveTask("extract", "/u/照片备份-2026.zip", "ai", 10_000)
	tk.resetItems(10_000, 3)
	tk.setItem("2026/09/IMG_0001.HEIC", 1000)
	tk.add(400)
	a.tasksFlipped = true
	tt.Frame()
	tt.Advance(flipDuration + 50*time.Millisecond)
	tt.Frame()
	for _, want := range []string{"解压 照片备份-2026.zip", "AI", "进行中", "2026/09/IMG_0001.HEIC", "400 B / 1000 B"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if err := tt.Click("暂停"); err != nil {
		t.Fatal(err)
	}
	if taskManager().Get(tk.r.ID()).State != tasks.Paused {
		t.Fatal("pause")
	}
	tt.Frame()
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if !tk.isCanceled() {
		t.Fatal("cancel")
	}
}

func TestTasksViewFailedDetail(t *testing.T) {
	a, tt := tasksViewFixture(t)
	tk := startArchiveTask("extract", "/u/project-src.tar.gz", "user", 1000)
	tk.r.Item("src/vendor/big.bin", 10)
	tk.r.Fail("src/vendor/big.bin", errors.New("磁盘空间不足"))
	tk.finish(nil)
	a.tasksFlipped, a.tasksTab = true, 2
	tt.Frame()
	tt.Advance(flipDuration + 50*time.Millisecond)
	tt.Frame()
	if !tt.HasText("部分失败") || !tt.HasText("失败 1") {
		t.Fatalf("failed tab: %v", tt.Texts())
	}
	if err := tt.Click("失败 1 项"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("磁盘空间不足") {
		t.Fatalf("detail: %v", tt.Texts())
	}
}

// TestTasksViewShots writes TASKSYS 04–06 when SUNVALE_SHOT_DIR is set.
func TestTasksViewShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	a, tt := tasksViewFixture(t)
	now := time.Now()
	old := archiveNow
	archiveNow = func() time.Time { return now }
	t.Cleanup(func() { archiveNow = old })
	x := startArchiveTask("extract", "/home/u/Downloads/照片备份-2026.zip", "user", 812<<20)
	x.resetItems(812<<20, 3872)
	for _, n := range []string{"2026/09/IMG_20260918_182955.HEIC", "2026/09/IMG_20260918_183002.HEIC", "2026/09/VID_20260918_183010.MOV"} {
		x.setItem(n, 3<<20)
		x.add(3 << 20)
	}
	x.setItem("2026/09/VID_20260918_183021.MOV", 31<<20)
	y := startArchiveTask("compress", "/home/u/Documents/季度报告.zip", "ai", 48<<20)
	y.resetItems(48<<20, 86)
	y.setItem("季度报告/附件/销售明细-Q3.xlsx", 6<<20)
	now = now.Add(40 * time.Second)
	x.r.AddTotal(330 << 20)
	x.r.AddItem(12 << 20)
	y.r.AddTotal(20 << 20)
	y.r.AddItem(2 << 20)
	z := startArchiveTask("extract", "/home/u/Downloads/project-src.tar.gz", "user", 96<<20)
	z.resetItems(96<<20, 1204)
	for i, n := range []string{"src/vendor/big.bin", "src/assets/logo.psd", "src/.git/objects/pack/pack-7f3a.pack"} {
		z.r.Item(n, 1)
		z.r.Fail(n, errors.New([]string{"磁盘空间不足：还需 65 MB", "磁盘空间不足", "权限不足"}[i]))
	}
	z.add(31 << 20)
	z.finish(nil)
	save := func(name string) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_ = png.Encode(f, tt.Image())
	}
	tt.Frame()
	a.tasksFlipped = true
	tt.Frame()
	tt.Advance(flipDuration * 2 / 5)
	tt.Frame()
	save("04-flip-mid.png")
	tt.Advance(flipDuration)
	tt.Frame()
	a.tasksOpen = map[string]bool{x.r.ID() + "r": true}
	tt.Frame()
	tt.Frame()
	save("05-tasks-extracting.png")
	a.tasksTab = 2
	a.tasksOpen[z.r.ID()+"f"] = true
	tt.Frame()
	tt.Frame()
	save("06-tasks-failed.png")
}
