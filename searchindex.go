package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sunvale/internal/search"
	"sunvale/internal/searchquery"
)

// searchindex.go owns the application's index instance and its Chinese status
// language. The index itself lives in internal/search; roots stay here because
// they come from the host through globalSearchRoots.
var searchIndex = newSearchIndex(defaultSearchIndexCacheFile())

func defaultSearchIndexCacheFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "index.json")
}

func newSearchIndex(cacheFile string) *search.Index {
	return search.NewIndex(search.IndexConfig{
		CacheFile: cacheFile, PreferHome: true,
	})
}

// startSearchIndex warms the palette from cache and replaces stale snapshots
// in the background once the first window is ready.
func startSearchIndex() {
	searchIndex.Start(globalSearchRoots())
}

func searchIndexOn() bool { return searchIndex.Enabled() }

func searchIndexStale() bool { return searchIndex.Stale() }

func searchIndexRebuild() { searchIndex.Rebuild(globalSearchRoots()) }

// searchIndexNote folds the pane's latest listing into the persistent index.
func searchIndexNote(dir string, entries []Entry) { searchIndex.Note(dir, entries) }

func searchIndexQuery(roots []string, query searchquery.Query, hidden bool) ([]Entry, bool) {
	return searchIndex.Query(roots, query, hidden)
}

func searchIndexWhole() bool { return searchIndex.Whole() }

func flushSearchIndex() { searchIndex.Flush() }

// searchIndexStatus is the UI adapter's compact status shape.
type searchIndexStatus struct {
	on       bool
	total    int
	built    time.Time
	complete bool
	building bool
	progress int
	estimate int
}

func searchIndexState() searchIndexStatus {
	st := searchIndex.Status()
	return searchIndexStatus{
		on:       st.On,
		total:    st.Total,
		built:    st.Built,
		complete: st.Complete,
		building: st.Building,
		progress: st.Progress,
		estimate: st.Estimate,
	}
}

// searchIndexSummary is the footer's word on the index: count, cache age and
// whether the snapshot is complete. Empty while the index is off.
func searchIndexSummary() string {
	st := searchIndexState()
	if !st.on {
		return ""
	}
	if st.building {
		return "索引 " + groupThousands(st.progress) + " 项"
	}
	var parts []string
	if st.total > 0 {
		parts = append(parts, "索引 "+groupThousands(st.total)+" 项"+searchIndexAge(st.built)+searchIndexCapNote(st))
	}
	if len(parts) == 0 {
		return "索引正在建立"
	}
	return strings.Join(parts, " · ")
}

// searchIndexAge is how old the cache is, in the footer's words.
func searchIndexAge(built time.Time) string {
	if built.IsZero() {
		return ""
	}
	switch d := time.Since(built); {
	case d < time.Minute:
		return "（刚写入的缓存）"
	case d < time.Hour:
		return "（" + itoa(int(d.Minutes())) + " 分钟前的缓存）"
	case d < 24*time.Hour:
		return "（" + itoa(int(d.Hours())) + " 小时前的缓存）"
	default:
		return "（" + itoa(int(d.Hours()/24)) + " 天前的缓存）"
	}
}

func searchIndexCapNote(st searchIndexStatus) string {
	if st.complete {
		return ""
	}
	return "（未完整）"
}

// groupThousands keeps index counts readable in the compact status line.
func groupThousands(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", strings.TrimPrefix(s, "-")
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}
