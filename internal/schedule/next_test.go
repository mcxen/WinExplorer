package schedule

import (
	"testing"
	"time"
)

func TestNextRunDaily(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	j := Job{Hour: 19, Minute: 0, Enabled: true}
	// Wednesday 2026-10-21 08:30 → same day 19:00
	from := time.Date(2026, 10, 21, 8, 30, 0, 0, loc)
	got := NextRun(j, from, loc)
	want := time.Date(2026, 10, 21, 19, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("NextRun = %v, want %v", got, want)
	}
	// After today's slot → tomorrow
	from = time.Date(2026, 10, 21, 19, 0, 30, 0, loc)
	got = NextRun(j, from, loc)
	want = time.Date(2026, 10, 22, 19, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("after fire NextRun = %v, want %v", got, want)
	}
}

func TestNextRunWeekdays(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	// Monday–Friday only; from Saturday morning → Monday 9:00
	j := Job{Hour: 9, Minute: 0, Weekdays: []int{
		int(time.Monday), int(time.Tuesday), int(time.Wednesday),
		int(time.Thursday), int(time.Friday),
	}}
	from := time.Date(2026, 10, 24, 8, 0, 0, 0, loc) // Saturday
	got := NextRun(j, from, loc)
	want := time.Date(2026, 10, 26, 9, 0, 0, 0, loc) // Monday
	if !got.Equal(want) {
		t.Fatalf("weekday NextRun = %v (%s), want %v (%s)", got, got.Weekday(), want, want.Weekday())
	}
}

func TestDueSkipsSameMinute(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 21, 19, 0, 15, 0, loc)
	j := Job{Hour: 19, Minute: 0, Enabled: true, LastRunAt: now}
	if Due(j, now, loc) {
		t.Fatal("Due should be false when LastRunAt is same minute")
	}
	j.LastRunAt = time.Time{}
	if !Due(j, now, loc) {
		t.Fatal("Due should be true when never run")
	}
	j.Enabled = false
	if Due(j, now, loc) {
		t.Fatal("disabled job not due")
	}
}

func TestCronFromDaily(t *testing.T) {
	if g := CronFromDaily(0, 19, nil); g != "0 19 * * *" {
		t.Fatalf("cron = %q", g)
	}
	if g := CronFromDaily(30, 7, []int{1, 2, 3, 4, 5}); g != "30 7 * * 1,2,3,4,5" {
		t.Fatalf("cron weekdays = %q", g)
	}
}
