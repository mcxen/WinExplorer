package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchName(t *testing.T) {
	cases := []struct {
		name, pat string
		want      bool
	}{
		{"a.pdf", "", true},
		{"a.pdf", "*", true},
		{"a.pdf", ".pdf", true},
		{"a.PDF", "pdf", true},
		{"a.docx", ".pdf,.docx", true},
		{"a.txt", ".pdf", false},
		{"report_final.docx", "report*", true},
		{"notes.md", "*.md", true},
	}
	for _, c := range cases {
		if got := MatchName(c.name, c.pat); got != c.want {
			t.Errorf("MatchName(%q,%q)=%v want %v", c.name, c.pat, got, c.want)
		}
	}
}

func TestPlanFileRuleDry(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dl")
	dst := filepath.Join(dir, "docs")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.pdf", "b.txt", "c.docx"} {
		if err := os.WriteFile(filepath.Join(src, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(src, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := Job{
		SourceDir: src, DestDir: dst, Match: ".pdf,.docx", Op: OpMove,
	}
	now := time.Date(2026, 10, 21, 19, 0, 0, 0, time.Local)
	ops, err := PlanFileRule(j, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 {
		t.Fatalf("ops=%d want 2: %+v", len(ops), ops)
	}
	j.DateStamp = true
	j.Op = OpCopy
	ops, err = PlanFileRule(j, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 {
		t.Fatalf("datestamp ops=%d", len(ops))
	}
	for _, p := range ops {
		if filepath.Base(filepath.Dir(p.Dst)) != "2026-10-21" {
			t.Fatalf("dest should be under date folder: %s", p.Dst)
		}
		if p.Op != OpCopy {
			t.Fatalf("op=%s", p.Op)
		}
	}
}

func TestApplyFileRuleMove(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "dl")
	dst := filepath.Join(dir, "keep")
	_ = os.MkdirAll(src, 0o755)
	_ = os.MkdirAll(dst, 0o755)
	path := filepath.Join(src, "note.pdf")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := []PlannedOp{{Src: path, Dst: filepath.Join(dst, "note.pdf"), Op: OpMove}}
	n, err := ApplyFileRule(ops)
	if err != nil || n != 1 {
		t.Fatalf("Apply n=%d err=%v", n, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("source should be gone")
	}
	if _, err := os.Stat(filepath.Join(dst, "note.pdf")); err != nil {
		t.Fatal(err)
	}
}
