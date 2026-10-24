package main

import (
	"os"
	"time"

	"github.com/egoist/mygo/ui"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenUIMorePaints(t *testing.T) {
	doc := filepath.ToSlash(filepath.Join(t.TempDir(), "report.txt"))
	if err := os.WriteFile(doc, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	text := "```openui\nroot = Stack([s, f, st, g])\n" +
		"s = Steps([Step(\"扫描\", \"完成\", \"done\"), Step(\"清理\", \"\", \"active\")], \"进度\")\n" +
		"f = Files([FileCard(\"" + doc + "\", \"重复\")], \"文件\")\n" +
		"st = StatCards([StatCard(\"已用\", \"412 GB\", \"+12 GB\", \"warning\")])\n" +
		"g = Gallery([\"" + doc + "\"], \"相册\")\n```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 700)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"进度", "扫描", "清理", "文件", "report.txt", "已用", "412 GB", "+12 GB", "相册"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	meta := false
	for _, s := range tt.Texts() {
		meta = meta || (strings.HasPrefix(s, "5 B · ") && strings.HasSuffix(s, " · 重复"))
	}
	if !meta {
		t.Fatalf("file meta missing in %v", tt.Texts())
	}
	if err := tt.Click("预览 report.txt"); err != nil {
		t.Fatal(err)
	}
	if !a.quickLook || a.quickLookFile != doc {
		t.Fatalf("quick look %v %q", a.quickLook, a.quickLookFile)
	}
}

func openMoreFixture(t *testing.T, cols string) (*App, *ui.Tester, string, string, *[]string, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	doc := filepath.ToSlash(filepath.Join(dir, "report.txt"))
	sub := filepath.ToSlash(filepath.Join(dir, "sub"))
	if err := os.WriteFile(doc, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	opened := &[]string{}
	now := &time.Time{}
	*now = time.Unix(1_800_000_000, 0)
	oldOpen, oldNow := openUISysOpen, openUINow
	openUISysOpen = func(p string) error { *opened = append(*opened, p); return nil }
	openUINow = func() time.Time { return *now }
	openGalleryClick = openGalleryPending{}
	t.Cleanup(func() { openUISysOpen, openUINow = oldOpen, oldNow })
	a := newStoreApp(t, dir)
	text := "```openui\nroot = Stack([f, g])\n" +
		"f = Files([FileCard(\"" + sub + "\")], \"文件\")\n" +
		"g = Gallery([\"" + doc + "\"], \"相册\"" + cols + ")\n```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 700)
	tt.Frame()
	tt.Frame()
	return a, tt, doc, sub, opened, now
}

func TestOpenUIGalleryClickAndDoubleClick(t *testing.T) {
	a, tt, doc, _, opened, now := openMoreFixture(t, ", 2")
	if err := tt.Click("预览 report.txt"); err != nil {
		t.Fatal(err)
	}
	if a.quickLook {
		t.Fatal("quick look opened before the double-click window closed")
	}
	*now = now.Add(openClickDelay + time.Millisecond)
	tt.Frame()
	if !a.quickLook || a.quickLookFile != doc || len(*opened) != 0 {
		t.Fatalf("single click: quick %v %q opened %v", a.quickLook, a.quickLookFile, *opened)
	}
}

func TestOpenUIGalleryDoubleClickOpensSystemApp(t *testing.T) {
	a, tt, doc, _, opened, now := openMoreFixture(t, "")
	_ = tt.Click("预览 report.txt")
	_ = tt.Click("预览 report.txt")
	*now = now.Add(time.Second)
	tt.Frame()
	if a.quickLook || len(*opened) != 1 || (*opened)[0] != doc {
		t.Fatalf("double click: quick %v opened %v", a.quickLook, *opened)
	}
}

func TestOpenUIFilesFolderIconNavigates(t *testing.T) {
	a, tt, _, sub, opened, _ := openMoreFixture(t, "")
	if err := tt.Click("进入 sub"); err != nil {
		t.Fatal(err)
	}
	if got := filepath.ToSlash(a.cur().path); got != sub || len(*opened) != 0 {
		t.Fatalf("path %q opened %v", got, *opened)
	}
}

func TestOpenUIFilesStatCached(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := openUINow
	openUINow = func() time.Time { return now }
	t.Cleanup(func() { openUINow = old })
	p := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(p, []byte("1"), 0o644)
	if st, _ := openStat(p); st == nil || st.Size() != 1 {
		t.Fatal("first stat")
	}
	_ = os.WriteFile(p, []byte("12345"), 0o644)
	if st, _ := openStat(p); st.Size() != 1 {
		t.Fatal("cache missed within TTL")
	}
	now = now.Add(openStatTTL)
	if st, _ := openStat(p); st.Size() != 5 {
		t.Fatal("cache not refreshed after TTL")
	}
}

func TestOpenGalleryLayout(t *testing.T) {
	if c, w := openGalleryLayout(400, 2, 6); c != 2 || w != 196 {
		t.Fatalf("2 cols: %d %v", c, w)
	}
	if c, _ := openGalleryLayout(200, 6, 6); c != 1 {
		t.Fatalf("narrow fallback: %d", c)
	}
	if c, w := openGalleryLayout(600, 0, 2); c != 4 || w != openGalleryTileW {
		t.Fatalf("auto few: %d %v", c, w)
	}
}
