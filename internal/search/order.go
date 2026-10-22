package search

import (
	"os"
	"slices"
	"strings"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

// sortHitsByQuery applies the same relevance order to an index snapshot as the
// palette applies to streamed hits. Name and path break score ties.
func sortHitsByQuery(hits []fsentry.Entry, query searchquery.Query) {
	now := time.Now()
	home := ""
	if value, err := os.UserHomeDir(); err == nil {
		home = value
	}
	scores := make(map[string]int, len(hits))
	for _, hit := range hits {
		scores[hit.Path], _, _ = query.Score(hit.Name, hit.Path, hit.IsDir, hit.ModTime, now, home, "")
	}
	slices.SortStableFunc(hits, func(x, y fsentry.Entry) int {
		if c := scores[y.Path] - scores[x.Path]; c != 0 {
			return c
		}
		if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
			return c
		}
		return strings.Compare(x.Path, y.Path)
	})
}
