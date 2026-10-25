package applog

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []string{
		"Authorization: Bearer sk-abcdefghijklmnopqrstuvwxyz",
		`{"api_key":"supersecretvalue"}`,
		"https://x.com/v1?key=AIzaSyA1234567890abcdefghij&q=1",
		"token=abc123def456",
		"ghp_abcdefghijklmnopqrstuvwxyz0123",
	}
	for _, c := range cases {
		got := Redact(c)
		for _, bad := range []string{"sk-abcdefghij", "supersecretvalue", "AIzaSyA123", "abc123def456", "ghp_abcdef"} {
			if strings.Contains(got, bad) {
				t.Errorf("Redact(%q) = %q leaks %q", c, got, bad)
			}
		}
	}
	if got := Redact(`C:\Users\mcxen\a.txt`); got != `C:\Users\mcxen\a.txt` {
		t.Errorf("path changed: %q", got)
	}
	if got := Trunc("abcdef", 3); !strings.HasPrefix(got, "abc…") {
		t.Errorf("Trunc = %q", got)
	}
}

func TestHandlerRedactsAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(&redactHandler{slog.NewTextHandler(&buf, nil)})
	l.Info("req", "apiKey", "sk-live-123456789012345", "url", "https://h/v1?api_key=zzzz", "body", "Bearer abcdefghijklmn")
	s := buf.String()
	for _, bad := range []string{"sk-live", "zzzz", "abcdefghijklmn"} {
		if strings.Contains(s, bad) {
			t.Fatalf("leak %q in %s", bad, s)
		}
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	r, err := NewRotator(p, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 39) + "\n")
	for i := 0; i < 20; i++ {
		r.Write(line)
	}
	r.Close()
	for _, n := range []string{p, p + ".1", p + ".2"} {
		st, err := os.Stat(n)
		if err != nil {
			t.Fatalf("missing %s", n)
		}
		if st.Size() > 100 {
			t.Errorf("%s size %d > 100", n, st.Size())
		}
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Error("kept more than 2 backups")
	}
	if got := len(Files(dir)); got != 3 {
		t.Errorf("Files = %d", got)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	lv := new(slog.LevelVar)
	l := slog.New(&redactHandler{slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lv})})
	lv.Set(ParseLevel("warn"))
	l.Info("hidden-info")
	l.Debug("hidden-debug")
	l.Warn("shown-warn")
	l.Error("shown-error")
	s := buf.String()
	if strings.Contains(s, "hidden") || !strings.Contains(s, "shown-warn") || !strings.Contains(s, "shown-error") {
		t.Fatalf("filter wrong: %s", s)
	}
	for _, n := range Levels {
		if LevelName(ParseLevel(n)) != n {
			t.Errorf("roundtrip %s", n)
		}
	}
}
