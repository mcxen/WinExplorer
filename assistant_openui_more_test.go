package main

import (
	"os"
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
