package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// assist_missing_dir_test.go: missing folders must fail fast. A deleted tab
// path used as Dialog.Open DefaultPath used to hang GTK and leave
// assist.picking stuck; tools that hit a gone cwd must return an error
// within a couple of seconds, not retry or block the UI thread.

func TestAssistDialogDirMissingFallsBack(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone", "deeper", "nope")
	dir := assistDialogDir(missing)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("assistDialogDir(%q)=%q: %v", missing, dir, err)
	}
	for _, path := range []string{"", pathThisPC, pathHome} {
		got := assistDialogDir(path)
		if st, err := os.Stat(got); err != nil || !st.IsDir() {
			t.Fatalf("assistDialogDir(%q)=%q: %v", path, got, err)
		}
	}
}

func TestAssistPickClearsOnErrorAndWatchdog(t *testing.T) {
	a := &App{assist: assistantUI{}}
	a.runAssistPick(func() ([]string, error) {
		return nil, context.Canceled
	}, func(_ []string, err error) {
		if err == nil {
			t.Error("expected error")
		}
	})
	waitPickingClear(t, a, 2*time.Second)

	old := assistPickWatchdog
	assistPickWatchdog = 80 * time.Millisecond
	defer func() { assistPickWatchdog = old }()
	a.assist.picking = false
	block := make(chan struct{})
	a.runAssistPick(func() ([]string, error) {
		<-block
		return nil, nil
	}, nil)
	waitPickingClear(t, a, 2*time.Second)
	close(block)
}

func waitPickingClear(t *testing.T, a *App, budget time.Duration) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if !a.assist.picking {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("picking still true after %s", budget)
}

func TestAssistToolsMissingDirFast(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	sunvaleToolState.bind(missing, false, nil, nil, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		tool    string
		args    map[string]any
		wantErr bool
		want    string
	}{
		{"list_dir", "list_dir", map[string]any{"path": ""}, true, ""},
		{"list_outside", "list_outside", map[string]any{"path": missing}, true, ""},
		{"folder_summary", "folder_summary", map[string]any{"path": missing}, true, ""},
		{"navigate_to", "navigate_to", map[string]any{"path": missing}, true, ""},
		{"bash", "bash", map[string]any{"script": "ls"}, true, "工作目录不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			res := callTool(t, sess, tc.tool, tc.args)
			elapsed := time.Since(start)
			if elapsed > 2*time.Second {
				t.Fatalf("%s blocked %s, want <2s", tc.tool, elapsed)
			}
			text := toolResultText(res)
			if tc.wantErr && !res.IsError {
				t.Fatalf("%s: want IsError, got %q (%s)", tc.tool, text, elapsed)
			}
			if tc.want != "" && !strings.Contains(text, tc.want) {
				t.Fatalf("%s text = %q, want %q", tc.tool, text, tc.want)
			}
		})
	}

	// read_file reports the OS error in the text body (not always IsError).
	t.Run("read_file", func(t *testing.T) {
		start := time.Now()
		res := callTool(t, sess, "read_file", map[string]any{"path": filepath.Join(missing, "x.txt")})
		elapsed := time.Since(start)
		if elapsed > 2*time.Second {
			t.Fatalf("read_file blocked %s", elapsed)
		}
		text := toolResultText(res)
		if res.IsError {
			return
		}
		if !strings.Contains(text, "no such file") && !strings.Contains(text, "不存在") {
			t.Fatalf("read_file missing: %q", text)
		}
	})
}

func TestRunBashMissingRootFast(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-shell-cwd")
	start := time.Now()
	_, err := runBash(context.Background(), missing, "ls")
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("runBash blocked %s", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "工作目录不存在") {
		t.Fatalf("err = %v", err)
	}
}
