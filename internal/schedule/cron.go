package schedule

import (
	"fmt"
	"strconv"
	"strings"
)

// CronFromDaily builds a 5-field cron (min hour * * dow) from a daily
// local clock. Empty weekdays mean every day (*).
func CronFromDaily(minute, hour int, weekdays []int) string {
	dow := "*"
	if len(weekdays) > 0 {
		parts := make([]string, 0, len(weekdays))
		seen := map[int]bool{}
		for _, d := range weekdays {
			if d < 0 || d > 6 || seen[d] {
				continue
			}
			seen[d] = true
			parts = append(parts, strconv.Itoa(d))
		}
		if len(parts) > 0 {
			dow = strings.Join(parts, ",")
		}
	}
	return fmt.Sprintf("%d %d * * %s", minute, hour, dow)
}
