package schedule

import (
	"time"
)

// NextRun returns the next local fire time at or after from (exclusive of
// an exact equal second — a job that just fired at from itself is not
// returned again until the following slot). from should already be in loc
// or have a usable Instant; the clock fields are interpreted in loc.
func NextRun(j Job, from time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	from = from.In(loc)
	// Start searching from the next minute so a fire at exactly HH:MM:00
	// does not re-queue in the same minute tick.
	cursor := from.Truncate(time.Minute).Add(time.Minute)

	allow := weekdaySet(j.Weekdays)
	for n := 0; n < 366*24*60; n++ { // one year of minutes max
		if cursor.Hour() == j.Hour && cursor.Minute() == j.Minute {
			if allow == nil || allow[cursor.Weekday()] {
				return time.Date(cursor.Year(), cursor.Month(), cursor.Day(),
					j.Hour, j.Minute, 0, 0, loc)
			}
		}
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}
}

// Due reports whether j should fire for the minute that contains now
// (truncated to the minute). Used by the process ticker.
func Due(j Job, now time.Time, loc *time.Location) bool {
	if !j.Enabled {
		return false
	}
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	if now.Hour() != j.Hour || now.Minute() != j.Minute {
		return false
	}
	allow := weekdaySet(j.Weekdays)
	if allow != nil && !allow[now.Weekday()] {
		return false
	}
	// Skip if we already recorded a successful/attempted run in this minute.
	if !j.LastRunAt.IsZero() {
		last := j.LastRunAt.In(loc)
		if last.Year() == now.Year() && last.YearDay() == now.YearDay() &&
			last.Hour() == now.Hour() && last.Minute() == now.Minute() {
			return false
		}
	}
	return true
}

func weekdaySet(days []int) map[time.Weekday]bool {
	if len(days) == 0 {
		return nil
	}
	m := map[time.Weekday]bool{}
	for _, d := range days {
		if d >= 0 && d <= 6 {
			m[time.Weekday(d)] = true
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
