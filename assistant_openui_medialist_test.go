package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMediaListPaintsAndPreviews(t *testing.T) {
	doc := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(doc, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	text := "```openui\nroot = MediaList([m1], \"步骤\", \"numbered\")\nm1 = MediaItem(\"" +
		filepath.ToSlash(doc) + "\", \"整理笔记\", \"合并为一份。\", \"doc\")\n```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 700)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"步骤", "1. 整理笔记", "合并为一份。"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if err := tt.Click("预览 notes.txt"); err != nil {
		t.Fatal(err)
	}
	if !a.quickLook || a.singleSelected().Path != filepath.ToSlash(doc) {
		t.Fatalf("quick look %v %q", a.quickLook, a.quickLookFile)
	}
}
