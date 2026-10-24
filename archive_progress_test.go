package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func isolateArchiveTasks(t *testing.T) {
	t.Helper()
	archiveTasks.Lock()
	old := archiveTasks.list
	archiveTasks.list = nil
	archiveTasks.Unlock()
	t.Cleanup(func() {
		archiveTasks.Lock()
		archiveTasks.list = old
		archiveTasks.Unlock()
	})
}

func archiveFixture(t *testing.T) (string, []Entry, int64) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	_ = os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a.bin"), bytes.Repeat([]byte("a"), 70_000), 0o644)
	_ = os.WriteFile(filepath.Join(src, "sub", "b.bin"), bytes.Repeat([]byte("b"), 30_000), 0o644)
	return root, []Entry{{Name: "src", Path: src, IsDir: true}}, 100_000
}

func TestArchiveProgressCountsZipBytes(t *testing.T) {
	isolateArchiveTasks(t)
	root, entries, size := archiveFixture(t)
	if got := entriesSize(entries); got != size {
		t.Fatalf("entriesSize = %d", got)
	}
	dst := filepath.Join(root, "out.zip")
	tk := startArchiveTask("compress", dst, "user", 0)
	if err := makeZip(dst, entries); err != nil {
		t.Fatal(err)
	}
	if s := tk.snap(); s.Done != size || s.Total != size || s.Cur == "" {
		t.Fatalf("compress snap %+v", s)
	}
	tk.finish(nil)
	if s := tk.snap(); !s.Finished || s.Frac() != 1 || s.Cur != "" {
		t.Fatalf("finished snap %+v", s)
	}
	ex := startArchiveTask("extract", dst, "user", 0)
	if err := extractZip(dst, filepath.Join(root, "x")); err != nil {
		t.Fatal(err)
	}
	if s := ex.snap(); s.Done != size || s.Total != size || !strings.HasSuffix(s.Cur, ".bin") {
		t.Fatalf("extract snap %+v", s)
	}
}

func TestArchiveProgressTarGz(t *testing.T) {
	isolateArchiveTasks(t)
	root := t.TempDir()
	tgz := filepath.Join(root, "a.tar.gz")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := bytes.Repeat([]byte("z"), 5000)
	_ = tw.WriteHeader(&tar.Header{Name: "z.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gw.Close()
	_ = os.WriteFile(tgz, buf.Bytes(), 0o644)
	tk := startArchiveTask("extract", tgz, "ai", 0)
	dst := filepath.Join(root, "o")
	_ = os.MkdirAll(dst, 0o755)
	if err := extractArchive(tgz, dst); err != nil {
		t.Fatal(err)
	}
	if s := tk.snap(); s.Total != int64(buf.Len()) || s.Done != s.Total || s.Cur != "z.txt" {
		t.Fatalf("tgz snap %+v (size %d)", s, buf.Len())
	}
}

func TestArchiveProgressCancel(t *testing.T) {
	isolateArchiveTasks(t)
	root, entries, _ := archiveFixture(t)
	dst := filepath.Join(root, "c.zip")
	tk := startArchiveTask("compress", dst, "user", 0)
	tk.cancel()
	err := makeZip(dst, entries)
	if !errors.Is(err, errArchiveCanceled) {
		t.Fatalf("err = %v", err)
	}
	tk.finish(err)
	if s := tk.snap(); s.Err != "已取消" || !s.Canceled {
		t.Fatalf("snap %+v", s)
	}
}

func TestArchiveMetaSpeedETA(t *testing.T) {
	isolateArchiveTasks(t)
	now := time.Unix(1_800_000_000, 0)
	old := archiveNow
	archiveNow = func() time.Time { return now }
	t.Cleanup(func() { archiveNow = old })
	tk := startArchiveTask("extract", "/d/a.zip", "user", 10<<20)
	tk.add(4 << 20)
	now = now.Add(2 * time.Second)
	s := tk.snap()
	if s.ETA != 3*time.Second {
		t.Fatalf("eta %v", s.ETA)
	}
	if m := archiveMeta(s); m != "40% · 4.0 MB / 10 MB · 2.0 MB/s · 剩余 0:03" {
		t.Fatalf("meta %q", m)
	}
}

func TestArchiveStatusBarCancels(t *testing.T) {
	isolateArchiveTasks(t)
	a := newTestApp(t, t.TempDir())
	tk := startArchiveTask("extract", "/d/photos.zip", "user", 1000)
	tk.add(500)
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(appTheme(c))
		ui.Column(c).Fill().Children(func() { a.statusBar(c) })
	}, 900, 40)
	tt.Frame()
	found := false
	for _, s := range tt.Texts() {
		found = found || strings.HasPrefix(s, "解压 photos.zip  50%")
	}
	if !found {
		t.Fatalf("no strip in %v", tt.Texts())
	}
	if err := tt.Click("取消解压 photos.zip"); err != nil {
		t.Fatal(err)
	}
	if !tk.isCanceled() {
		t.Fatal("cancel not set")
	}
}

func TestAssistantShowsArchiveTask(t *testing.T) {
	isolateArchiveTasks(t)
	a := &App{}
	at := time.Now().Add(-time.Minute)
	a.assist.turns = []aiTurn{{Role: "user", Text: "解压", At: at}, {Role: "assistant", Text: "好的", At: at}}
	tk := startArchiveTask("extract", "/d/big.zip", "ai", 2000)
	tk.add(1000)
	tk.setFile("docs/readme.md")
	startArchiveTask("extract", "/d/user.zip", "user", 10)
	tt := assistTester(a, 700)
	tt.Frame()
	for _, want := range []string{"解压 big.zip", "docs/readme.md"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if tt.HasText("解压 user.zip") {
		t.Fatal("the user's own task shows in the assistant")
	}
	tk.finish(errors.New("磁盘已满"))
	tt.Frame()
	if !tt.HasText("磁盘已满") {
		t.Fatalf("error missing in %v", tt.Texts())
	}
}

func TestOpenUITaskProgressPaints(t *testing.T) {
	a := &App{}
	text := "```openui\nroot = Stack([TaskProgress(\"解压 a.zip\", \"42%\", \"8 MB/s\", \"docs/x.pdf\", \"active\"), TaskProgress(\"压缩 b\", 1, \"\", \"\", \"done\")])\n```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 500)
	tt.Frame()
	for _, want := range []string{"解压 a.zip", "8 MB/s", "docs/x.pdf", "压缩 b", "100%"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
}
