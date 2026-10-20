package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantSpecifiedFilesAndChoices(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("alpha beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep.txt"), []byte("find-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), bytes.Repeat([]byte("x"), 40*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(outside, "out.txt")
	if err := os.WriteFile(other, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(dir, ".ssh")
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "id_rsa")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	sunvaleToolState.bind(dir, false, nil, []string{filepath.Join(dir, "note.txt")}, false)
	ctx := context.Background()
	sess, err := sunvaleSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got := callToolText(t, sess, "read_files", map[string]any{"paths": []string{"note.txt", other}})
	if !strings.Contains(got, "alpha beta") || !strings.Contains(got, "outside") || !strings.Contains(got, other) {
		t.Fatalf("read_files: %s", got)
	}
	bigText := callToolText(t, sess, "read_file", map[string]any{"path": "big.txt"})
	if !strings.Contains(bigText, "已截断") || len(bigText) > 40*1024 {
		t.Fatalf("big read len %d: %s", len(bigText), bigText[:min(80, len(bigText))])
	}
	found := callToolText(t, sess, "search_names", map[string]any{"query": "deep"})
	if !strings.Contains(found, "deep.txt") {
		t.Fatalf("search: %s", found)
	}
	grep := callToolText(t, sess, "grep_text", map[string]any{"query": "alpha", "path": "note.txt"})
	if !strings.Contains(grep, "note.txt") || !strings.Contains(grep, "alpha beta") {
		t.Fatalf("grep: %s", grep)
	}
	sum := callToolText(t, sess, "folder_summary", map[string]any{})
	if !strings.Contains(sum, "文件:") || !strings.Contains(sum, "txt") {
		t.Fatalf("summary: %s", sum)
	}
	space := callToolText(t, sess, "disk_space", map[string]any{})
	if !strings.Contains(space, "总共") && !strings.Contains(space, "读不到") {
		t.Fatalf("space: %s", space)
	}
	sel := callToolText(t, sess, "current_selection", map[string]any{})
	if !strings.Contains(sel, "note.txt") {
		t.Fatalf("selection: %s", sel)
	}
	wrote := callToolText(t, sess, "write_text", map[string]any{"path": "made.txt", "content": "new"})
	if !strings.Contains(wrote, "made.txt") {
		t.Fatalf("write: %s", wrote)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "made.txt")); err != nil || string(data) != "new" {
		t.Fatalf("wrote %q %v", data, err)
	}
	undos := sunvaleToolState.drainUndos()
	if len(undos) != 1 || len(undos[0].Created) != 1 {
		t.Fatalf("undo: %+v", undos)
	}
	// No path denylist: a credential-looking path is writable like any other.
	if res := callTool(t, sess, "write_text", map[string]any{"path": secret, "content": "nope"}); res.IsError {
		t.Fatalf("write to %s refused", secret)
	}
	if string(mustRead(t, secret)) != "nope" {
		t.Fatal("write did not land")
	}
	nav := callToolText(t, sess, "navigate_to", map[string]any{"path": "sub"})
	if got := sunvaleToolState.drainNav(); got != filepath.Join(dir, "sub") {
		t.Fatalf("nav %q tool %s", got, nav)
	}
	block := callToolText(t, sess, "offer_choices", map[string]any{
		"prompt":  "怎么整理？",
		"mode":    "multi",
		"options": []string{"按日期", "按类型"},
	})
	display, ch := parseChoices(block)
	if ch == nil || !ch.Multi || len(ch.Options) != 2 || ch.Prompt != "怎么整理？" || display != "" {
		t.Fatalf("choices display %q %+v from %s", display, ch, block)
	}
	note := specifiedFileNote([]string{filepath.Join(dir, "note.txt")})
	if !strings.Contains(note, "alpha beta") || !strings.Contains(note, "note.txt") {
		t.Fatalf("note: %s", note)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
