package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResolveInRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "work", "folder")
	got, err := resolveInRoot(root, "a.txt")
	if err != nil || got != filepath.Join(root, "a.txt") {
		t.Fatalf("got %s %v", got, err)
	}
	if _, err := resolveInRoot(root, ".."); err == nil {
		t.Fatal("parent escaped the folder")
	}
	if _, err := resolveInRoot(root, filepath.Join("..", "secret")); err == nil {
		t.Fatal("nested parent escaped the folder")
	}
}

func TestSunvaleMCPStaysInFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	sunvaleToolState.bind(dir, false, []archivePassword{{ID: "1", Label: "mail", Secret: "s3cret"}}, nil, false)
	ctx := context.Background()
	sess, err := sunvaleSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := officialmcp.GetTools(ctx, &officialmcp.Config{Cli: sess, ServerName: "sunvale"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		info, err := tool.Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		names[info.Name] = true
	}
	for _, want := range []string{"list_dir", "read_file", "read_outside", "list_outside", "mkdir", "copy_items", "move_items", "trash_items", "rename_item", "rename_plan", "zip_files", "unzip", "password_list"} {
		if !names[want] {
			t.Fatalf("eino did not receive %s: %v", want, names)
		}
	}

	text := callToolText(t, sess, "list_dir", map[string]any{"path": ""})
	if !strings.Contains(text, "a.txt") || !strings.Contains(text, "sub/") {
		t.Fatalf("list: %s", text)
	}
	if got := callToolText(t, sess, "read_file", map[string]any{"path": "a.txt"}); got != "hello" {
		t.Fatalf("read: %q", got)
	}
	escaped := callTool(t, sess, "read_file", map[string]any{"path": ".."})
	if !escaped.IsError {
		t.Fatal("read escaped the folder")
	}
	book := callToolText(t, sess, "password_list", map[string]any{})
	if !strings.Contains(book, "mail") || strings.Contains(book, "s3cret") {
		t.Fatalf("password list leaked or missed the name: %s", book)
	}
	plan := callToolText(t, sess, "rename_plan", map[string]any{"mode": "template", "pattern": "item-{num}"})
	id := planIDFrom(plan)
	if id == "" || !strings.Contains(plan, "a.txt") {
		t.Fatalf("plan: %s", plan)
	}
	if _, ok := sunvaleToolState.takePlan(id); !ok {
		t.Fatal("plan was not kept for 应用方案")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal("rename_plan changed the file")
	}

	outside := t.TempDir()
	outFile := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(outFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := callToolText(t, sess, "read_outside", map[string]any{"path": outFile}); got != "outside" {
		t.Fatalf("read_outside: %q", got)
	}
	listed := callToolText(t, sess, "list_outside", map[string]any{"path": outside})
	if !strings.Contains(listed, "note.txt") {
		t.Fatalf("list_outside: %s", listed)
	}
	if callTool(t, sess, "read_outside", map[string]any{"path": "note.txt"}).IsError != true {
		t.Fatal("read_outside accepted a relative path")
	}

	made := callToolText(t, sess, "mkdir", map[string]any{"name": "box"})
	if !strings.Contains(made, "box") {
		t.Fatalf("mkdir: %s", made)
	}
	callToolText(t, sess, "new_text_file", map[string]any{"name": "b.txt"})
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	copied := callToolText(t, sess, "copy_items", map[string]any{"names": []string{"a.txt"}, "dest": "box"})
	if !strings.Contains(copied, filepath.Join(dir, "box", "a.txt")) {
		t.Fatalf("copy: %s", copied)
	}
	renamed := callToolText(t, sess, "rename_item", map[string]any{"name": "b.txt", "to": "c.txt"})
	if !strings.Contains(renamed, "c.txt") {
		t.Fatalf("rename: %s", renamed)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("old name still there")
	}
}

func callTool(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func callToolText(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res := callTool(t, sess, name, args)
	if res.IsError || len(res.Content) == 0 {
		t.Fatalf("%s: %+v", name, res)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s content %T", name, res.Content[0])
	}
	return text.Text
}
