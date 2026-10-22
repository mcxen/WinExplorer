package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// assistant_shell_test.go drives the built-in shell: the interpreter's
// language, the Go coreutils standing in for what Windows lacks, the
// sandbox that bounds them, and the bash tool's end-to-end answer.

func TestRunBashCoreutils(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "photo.JPG", "photo2.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hello world\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{"ls", "ls", []string{"a.txt", "b.txt", "photo.JPG", "photo2.jpg", "sub/"}},
		{"grep pipe", "ls | grep jpg", []string{"photo2.jpg"}},
		{"for archive", `mkdir -p 归档; for f in *.jpg *.JPG; do mv "$f" 归档/; done; ls 归档`, []string{"photo.JPG", "photo2.jpg"}},
		{"count", "ls | wc -l", []string{"5"}},
		{"sed rename list", "find . -name '*.txt' | sed 's/^/x/' | wc -l", []string{"2"}},
		{"glob redirect", "ls *.txt > 清单.txt; cat 清单.txt", []string{"a.txt", "b.txt"}},
		{"conditional", "if [ -f a.txt ]; then echo 有; fi", []string{"有"}},
		{"head tail", "seq 1 20 | head -n 3", []string{"1", "2", "3"}},
		{"sort uniq", "printf 'b\\na\\nb\\n' | sort | uniq", []string{"a", "b"}},
		{"cut", "printf 'a,b,c\\n' | cut -d, -f2", []string{"b"}},
		{"du stat", "du .", []string{"."}},
		{"and-or", "true && echo yes || echo no", []string{"yes"}},
		{"while", "i=0; while [ $i -lt 3 ]; do echo n$i; i=$((i+1)); done", []string{"n0", "n1", "n2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each script gets a fresh copy of the fixture folder: the
			// earlier cases move files around.
			work := t.TempDir()
			copyTree(t, dir, work)
			got, err := runBash(context.Background(), work, tc.script)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.script, err, got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%s = %q, want %q", tc.script, got, want)
				}
			}
		})
	}
}

// TestRunBashSandbox: writes and reads stay inside the run's root.
func TestRunBashSandbox(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBash(context.Background(), dir, "echo hi > ../escape.txt"); err == nil {
		t.Fatal("an escape through redirection was allowed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("the escape file exists")
	}
	// Absolute paths outside the root are refused: the output is empty
	// and the refusal is said on stderr, while the script itself still
	// runs — one bad path does not sink the rest.
	got, err := runBash(context.Background(), dir, "cat /etc/passwd")
	if err != nil {
		t.Fatalf("the refusal should not fail the script: %v", err)
	}
	if got != "" || strings.Contains(got, "root:") {
		t.Fatalf("the outside file leaked: %q", got)
	}
}

// TestRunBashTimeout: a runaway script is stopped by the timeout.
func TestRunBashTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("the timeout test takes the full timeout")
	}
	dir := t.TempDir()
	_, err := runBash(context.Background(), dir, "while true; do echo spinning; done")
	if err == nil || !strings.Contains(err.Error(), "被停止") {
		t.Fatalf("err = %v", err)
	}
}

// TestBashTool: the assistant's tool answers with the script's output, and
// an empty script or a syntax error says so.
func TestBashTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("find-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	sunvaleToolState.bind(dir, false, nil, nil, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "bash", map[string]any{
		"script": "grep find-me note.txt",
		"why":    "核对文件里的一句话",
	})
	if !strings.Contains(got, "find-me") {
		t.Fatalf("bash: %s", got)
	}
	res := callTool(t, sess, "bash", map[string]any{"script": ""})
	if text := toolResultText(res); !res.IsError || !strings.Contains(text, "script") {
		t.Fatalf("empty script: %+v %s", res, text)
	}
	res = callTool(t, sess, "bash", map[string]any{"script": "if ["})
	if text := toolResultText(res); !res.IsError || !strings.Contains(text, "语法") {
		t.Fatalf("syntax error: %+v %s", res, text)
	}
}

// TestRunBashCapsOutput: a talkative script is cut, not infinite.
func TestRunBashCapsOutput(t *testing.T) {
	dir := t.TempDir()
	// A long heredoc loop is the talkative case: it would print megabytes
	// of padding if the cap did not stop growing.
	script := "i=0; while [ $i -lt 5000 ]; do echo 一二三四五六七八九十; i=$((i+1)); done"
	got, err := runBash(context.Background(), dir, script)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > shellOutputMax+64 {
		t.Fatalf("output = %d bytes, over the cap", len(got))
	}
	if !strings.Contains(got, "截断") {
		t.Fatalf("a capped run should say so: %d bytes", len(got))
	}
}

// TestBashToolWritesLightRows: a shell that moves files lights the
// rows it touches, the way the file tools do.
func TestBashToolWritesLightRows(t *testing.T) {
	fx, clk := swapTestFx(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sunvaleToolState.bind(dir, false, nil, nil, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callToolText(t, sess, "bash", map[string]any{"script": "mv note.txt 归档.txt"})
	moved := filepath.Join(dir, "归档.txt")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("mv did not land: %v", err)
	}
	clk.add(400 * time.Millisecond) // the breath fades in over 200 ms
	if l := fx.look(moved); l.glow <= 0 {
		t.Fatalf("the moved row did not light: %+v", l)
	}
	if l := fx.look(filepath.Join(dir, "note.txt")); l.any() {
		t.Fatalf("the source row lit without a move: %+v", l)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	ents, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyTree(t, s, d)
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
