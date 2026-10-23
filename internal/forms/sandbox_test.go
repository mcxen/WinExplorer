package forms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferPathFallsBackToSandbox(t *testing.T) {
	home, _ := os.UserHomeDir()
	missing := filepath.Join(home, "Downloads")
	if _, err := os.Stat(missing); err == nil {
		t.Skip("~/Downloads exists; cannot assert sandbox fallback")
	}
	p := PreferPath("Downloads")
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		t.Fatalf("PreferPath Downloads: %q err=%v", p, err)
	}
	report := PreferPath(filepath.Join("Documents", "报告.pdf"))
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("sample report: %v", err)
	}
	f := ScheduleAdd(nil)
	for _, id := range []string{"source", "dest"} {
		var val string
		for _, field := range f.Fields {
			if field.ID == id {
				val = field.Value
				break
			}
		}
		if _, err := os.Stat(val); err != nil {
			t.Fatalf("schedule %s=%q: %v", id, val, err)
		}
	}
	dir := ExistingDir(filepath.Join(home, "Downloads", "nope"))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("ExistingDir: %q %v", dir, err)
	}
}
