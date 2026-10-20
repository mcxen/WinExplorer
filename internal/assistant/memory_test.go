package assistant

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestMemoryAndSessionStorage(t *testing.T) {
	dir := t.TempDir()
	memoryPath := filepath.Join(dir, "memory.json")
	memory := []Memory{{ID: "1", Text: "下载按月份归档"}}
	if err := WriteMemories(memoryPath, memory); err != nil {
		t.Fatal(err)
	}
	gotMemory, err := ReadMemories(memoryPath, 40)
	if err != nil || !reflect.DeepEqual(gotMemory, memory) {
		t.Fatalf("memory %+v %v", gotMemory, err)
	}

	indexPath := filepath.Join(dir, "index.json")
	index := SessionIndex{ActiveID: "2", Sessions: []SessionMeta{{ID: "2", Title: "整理"}}}
	if err := WriteIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	gotIndex, ok := ReadIndex(indexPath)
	if !ok || !reflect.DeepEqual(gotIndex, index) {
		t.Fatalf("index %+v %v", gotIndex, ok)
	}
	if _, ok := ReadIndex(filepath.Join(dir, "missing.json")); !ok {
		t.Fatal("a missing index is a valid empty one")
	}

	turns := []Turn{
		{Role: "user", Text: "整理下载", Model: "private", Files: []Attach{{Name: "a.txt"}}},
		{Role: "assistant", Text: "完成"},
	}
	if title := SessionTitle(turns, 24); title != "整理下载" {
		t.Fatalf("title %q", title)
	}
	if preview := SessionPreview(turns, 42); preview != "完成" {
		t.Fatalf("preview %q", preview)
	}
	if prompt := MemoryPrompt([]Memory{{Text: "目录 {folder}"}}); prompt != "- 目录 (folder)" {
		t.Fatalf("prompt %q", prompt)
	}
	saved := DiskTurns(turns, 8000)
	if saved[0].Model != "" || saved[0].Text != "整理下载" || saved[0].Files[0] != (Attach{Name: "a.txt"}) {
		t.Fatalf("saved turns %+v", saved[0])
	}

	normalized := NormalizeTurns([]Turn{{Role: "user", Plan: "missing", Choice: &Choice{Options: []string{"a", "b"}}}}, 1, func(string) bool { return false })
	if normalized[0].Plan != "" || len(normalized[0].Choice.On) != 2 {
		t.Fatalf("normalized turn %+v", normalized[0])
	}
}
