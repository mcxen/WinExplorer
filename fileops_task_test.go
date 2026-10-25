package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sunvale/internal/tasks"
)

func asyncTasks(t *testing.T) {
	t.Helper()
	isolateArchiveTasks(t)
	old := taskAsync
	taskAsync = func() bool { return true }
	t.Cleanup(func() { taskAsync = old })
}

func waitAsk(t *testing.T, id string) tasks.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		s := taskManager().Get(id)
		if s.Ask != nil {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("no question; state %v", s.State)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A user copy into a folder that has the name asks in the card; 替换 for
// the rest replaces both clashes, the old files go to the bin, and undo
// brings everything back.
func TestCopyTaskAsksAndUndoes(t *testing.T) {
	asyncTasks(t)
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	_ = os.MkdirAll(src, 0o755)
	_ = os.MkdirAll(dst, 0o755)
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		_ = os.WriteFile(filepath.Join(src, n), []byte("new-"+n), 0o644)
	}
	_ = os.WriteFile(filepath.Join(dst, "a.txt"), []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(dst, "b.txt"), []byte("old"), 0o644)
	a := newStoreApp(t, dst)
	var got fileJobResult
	id := a.startFileJob(fileJob{Kind: tasks.KindCopy, Source: tasks.SourceUser,
		Srcs: []string{filepath.Join(src, "a.txt"), filepath.Join(src, "b.txt"), filepath.Join(src, "c.txt")}, DstDir: dst},
		func(r fileJobResult) { got = r; a.finishFileJob(r, "粘贴失败：") })
	s := waitAsk(t, id)
	if s.Ask.Item != "a.txt" || s.Ask.Rest != 2 {
		t.Fatalf("ask %+v", s.Ask)
	}
	if n := snapFromTask(s); n.Ask == nil {
		t.Fatal("card has no question")
	}
	_ = taskManager().Answer(id, tasks.Answer{Choice: tasks.ChoiceReplace, All: true})
	fin, _ := taskManager().Wait(context.Background(), id)
	if fin.State != tasks.Done || fin.ItemsDone != 3 || fin.Done != fin.Total || fin.Total == 0 {
		t.Fatalf("final %+v", fin)
	}
	for _, n := range []string{"a.txt", "b.txt"} {
		if b, _ := os.ReadFile(filepath.Join(dst, n)); string(b) != "new-"+n {
			t.Fatalf("%s = %q", n, b)
		}
	}
	if len(got.Placed) != 3 || len(a.undo) != 1 {
		t.Fatalf("placed %v undo %d", got.Placed, len(a.undo))
	}
	a.doUndo()
	if b, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(b) != "old" {
		t.Fatalf("undo left a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "c.txt")); err == nil {
		t.Fatal("undo kept the copy")
	}
}

// AI runs never ask: they keep both.
func TestCopyTaskAIKeepsBoth(t *testing.T) {
	isolateArchiveTasks(t)
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "x.txt"), []byte("1"), 0o644)
	dst := filepath.Join(root, "d")
	_ = os.MkdirAll(dst, 0o755)
	_ = os.WriteFile(filepath.Join(dst, "x.txt"), []byte("0"), 0o644)
	res := runFileJob(nil, fileJob{Kind: tasks.KindCopy, Source: tasks.SourceAI, Srcs: []string{filepath.Join(root, "x.txt")}, DstDir: dst})
	if len(res.Placed) != 1 || filepath.Base(res.Placed[0]) == "x.txt" {
		t.Fatalf("placed %v", res.Placed)
	}
}

// Cancel mid-question stops the copy and leaves nothing half done.
func TestCopyTaskCancelWhileAsking(t *testing.T) {
	asyncTasks(t)
	root := t.TempDir()
	dst := filepath.Join(root, "d")
	_ = os.MkdirAll(dst, 0o755)
	_ = os.WriteFile(filepath.Join(root, "x.txt"), []byte("1"), 0o644)
	_ = os.WriteFile(filepath.Join(dst, "x.txt"), []byte("0"), 0o644)
	a := newStoreApp(t, dst)
	id := a.startFileJob(fileJob{Kind: tasks.KindMove, Source: tasks.SourceUser, Srcs: []string{filepath.Join(root, "x.txt")}, DstDir: dst}, func(fileJobResult) {})
	waitAsk(t, id)
	_ = taskManager().Cancel(id)
	if s, _ := taskManager().Wait(context.Background(), id); s.State != tasks.Canceled {
		t.Fatalf("state %v", s.State)
	}
	if _, err := os.Stat(filepath.Join(root, "x.txt")); err != nil {
		t.Fatal("source moved after cancel")
	}
}

// Compress queues behind the disk slots and 重试 packs the same selection.
func TestCompressQueuesAndRetries(t *testing.T) {
	asyncTasks(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "r.txt"), []byte("hello"), 0o644)
	m := taskManager()
	gate := make(chan struct{})
	vol := archiveVolume(dir)
	for i := 0; i < 2; i++ {
		m.Submit(tasks.Spec{IO: "disk", Volume: vol}, func(*tasks.Reporter) error { <-gate; return nil })
	}
	a := newStoreApp(t, dir)
	a.selectOnly(filepath.Join(dir, "r.txt"))
	a.doCompress()
	var id string
	for _, s := range m.List() {
		if s.Kind == tasks.KindCompress {
			id = s.ID
		}
	}
	if id == "" || m.Get(id).State != tasks.Queued {
		t.Fatalf("compress not queued: %+v", m.Get(id))
	}
	_ = m.Cancel(id)
	close(gate)
	if s, _ := m.Wait(context.Background(), id); s.State != tasks.Canceled {
		t.Fatalf("compress %v %s", s.State, s.Err)
	}
	nid, err := m.Retry(id)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if s, _ := m.Wait(context.Background(), nid); s.State != tasks.Done {
		t.Fatalf("retry %v %s", s.State, s.Err)
	}
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	if len(zips) != 1 {
		t.Fatalf("zips %v", zips)
	}
}

// The clash is answered from the card: 保留两者 clicked in the Tasks view.
// With SUNVALE_SHOT_DIR set it also writes TASKSYS 07 (inline conflict)
// and 08 (queued tasks).
func TestCopyConflictCardAndQueueShots(t *testing.T) {
	a, tt := tasksViewFixture(t)
	asyncTasks(t)
	root := t.TempDir()
	src, dst := filepath.Join(root, "相机导入"), filepath.Join(root, "照片")
	_ = os.MkdirAll(src, 0o755)
	_ = os.MkdirAll(dst, 0o755)
	for _, n := range []string{"IMG_0412.HEIC", "IMG_0413.HEIC", "IMG_0414.HEIC", "IMG_0415.HEIC"} {
		_ = os.WriteFile(filepath.Join(src, n), make([]byte, 2<<20), 0o644)
	}
	_ = os.WriteFile(filepath.Join(dst, "IMG_0412.HEIC"), []byte("old"), 0o644)
	var srcs []string
	for _, n := range []string{"IMG_0412.HEIC", "IMG_0413.HEIC", "IMG_0414.HEIC", "IMG_0415.HEIC"} {
		srcs = append(srcs, filepath.Join(src, n))
	}
	m := taskManager()
	gate := make(chan struct{})
	defer close(gate)
	for i, n := range []string{"备份 Projects → 移动硬盘", "复制 素材库 → NAS", "移动 旧项目 → 归档", "删除 3 个项目"} {
		kind := []tasks.Kind{tasks.KindCopy, tasks.KindCopy, tasks.KindMove, tasks.KindDelete}[i]
		src := []tasks.Source{tasks.SourceSchedule, tasks.SourceUser, tasks.SourceAI, tasks.SourceUser}[i]
		m.Submit(tasks.Spec{Kind: kind, Title: n, Source: src, IO: "disk", Volume: "/mnt/backup"}, func(r *tasks.Reporter) error {
			r.SetTotal(tasks.UnitBytes, 4<<30, 1200)
			r.Item("Projects/sunvale/assets/demo.mov", 900<<20)
			r.AddTotal(1 << 30)
			r.AddItem(300 << 20)
			<-gate
			return nil
		})
	}
	id := a.startFileJob(fileJob{Kind: tasks.KindCopy, Source: tasks.SourceUser, Srcs: srcs, DstDir: dst}, func(fileJobResult) {})
	waitAsk(t, id)
	a.tasksFlipped = true
	tt.Frame()
	tt.Advance(flipDuration + 50*time.Millisecond)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("保留两者") || !tt.HasText("待选择") {
		t.Fatalf("no inline question: %v", tt.Texts())
	}
	shot := func(name string) {
		if d := os.Getenv("SUNVALE_SHOT_DIR"); d != "" {
			f, err := os.Create(filepath.Join(d, name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			_ = png.Encode(f, tt.Image())
		}
	}
	shot("07-copy-conflict.png")
	if err := tt.Click("保留两者"); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Wait(context.Background(), id); s.State != tasks.Done {
		t.Fatalf("copy %v %s", s.State, s.Err)
	}
	if ents, _ := os.ReadDir(dst); len(ents) != 5 {
		t.Fatalf("kept both? %d entries", len(ents))
	}
	a.tasksTab = 1
	tt.Frame()
	tt.Frame()
	if !tt.HasText("移动 旧项目 → 归档") {
		t.Fatalf("queue tab: %v", tt.Texts())
	}
	shot("08-tasks-queued.png")
}

// Mid-flip clicks land on the shield, not on either face.
func TestFlipIgnoresPointerMidTurn(t *testing.T) {
	a, tt := tasksViewFixture(t)
	tt.Frame()
	a.toggleTasksView()
	tt.Frame()
	tt.Advance(flipDuration / 5)
	tt.Frame()
	sel := len(a.cur().selected)
	_ = tt.Click("会议纪要.md")
	if len(a.cur().selected) != sel {
		t.Fatal("front face took a click mid-flip")
	}
}
