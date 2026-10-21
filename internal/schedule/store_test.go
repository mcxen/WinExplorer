package schedule

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, loc)
	j := Job{
		ID: "1", Name: "每晚整理下载", Enabled: true,
		Hour: 19, Minute: 0, Action: ActionFileRule,
		SourceDir: "/tmp/dl", DestDir: "/tmp/docs", Match: ".pdf", Op: OpMove,
	}
	if err := s.Upsert(j, now, loc); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Get("1")
	if !ok {
		t.Fatal("missing job")
	}
	if got.Name != j.Name || got.Cron != "0 19 * * *" {
		t.Fatalf("got %+v", got)
	}
	if got.NextRunAt.IsZero() {
		t.Fatal("NextRunAt empty")
	}
	if err := s2.SetEnabled("1", false, now, loc); err != nil {
		t.Fatal(err)
	}
	got, _ = s2.Get("1")
	if got.Enabled {
		t.Fatal("should be off")
	}
	if err := s2.Delete("1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get("1"); ok {
		t.Fatal("deleted")
	}
}
