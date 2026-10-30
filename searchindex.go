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

// The index file lives in the user cache dir: it is derived data, rebuilt
// from the disks at will (docs/specs/23-search-index.md).
func defaultSearchIndexCacheFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "index", "files.idx")
}

// searchIndexMaxEntries trades disk for time: a million rows is about 60 MB
// on disk and stays out of the Go heap (the file is mapped).
const searchIndexMaxEntries = 1_000_000

func newSearchIndex(cacheFile string) *search.Index {
	return search.NewIndex(search.IndexConfig{
		CacheFile: cacheFile, PreferHome: true, Watch: true,
		MaxEntries: searchIndexMaxEntries,
		Refresh:    30 * time.Minute, Throttle: 2 * time.Millisecond,
	})
}

// startSearchIndex maps the last index and refreshes a stale one in the
// background once the first window is ready. 设置 › 快速搜索 can switch it off.
func startSearchIndex(off bool) {
	removeLegacySearchIndex()
	if off {
		return
	}
	searchIndex.Start(globalSearchRoots())
}

// removeLegacySearchIndex deletes the JSON snapshot older builds kept in the
// config dir; the binary index replaced it.
func removeLegacySearchIndex() {
	if dir, err := os.UserConfigDir(); err == nil {
		_ = os.Remove(filepath.Join(dir, stateDirName, "index.json"))
	}
}

// setSearchIndexOn is the settings switch: off unmaps the index and stops
// its refresh, on maps it again and refreshes in the background.
func setSearchIndexOn(on bool) {
	if on {
		go searchIndex.Start(globalSearchRoots())
		return
	}
	go searchIndex.Disable()
}

// searchIndexTouch refreshes a stale snapshot behind an answered query.
func searchIndexTouch() {
	if searchIndex.Enabled() && searchIndex.Stale() {
		searchIndex.RebuildWith(globalSearchRoots)
	}
}

func searchIndexDirty(roots []string) []string { return searchIndex.Dirty(roots) }

func searchIndexComplete() bool { return searchIndex.Complete() }

func searchIndexOn() bool { return searchIndex.Enabled() }

func searchIndexStale() bool { return searchIndex.Stale() }

// searchIndexRebuild runs from click handlers: the volume list stats disks,
// so it is gathered off the UI thread (viewguard.go).
func searchIndexRebuild() { searchIndex.RebuildWith(globalSearchRoots) }

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
	bytes    int64
	took     time.Duration
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
		bytes:    st.Bytes,
		took:     st.Took,
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

// searchIndexSettingsLine is 设置 › 快速搜索's one-line index state: size on
// disk, rows, age and the last build's time, or the build's progress.
func searchIndexSettingsLine() string {
	st := searchIndexState()
	switch {
	case !st.on:
		return "已关闭，全局搜索逐次遍历磁盘"
	case st.building:
		return "正在更新 · 已扫描 " + groupThousands(st.progress) + " 项"
	case st.total == 0:
		return "尚未建立"
	}
	parts := []string{formatBytes(st.bytes), groupThousands(st.total) + " 项"}
	if !st.built.IsZero() {
		parts = append(parts, st.built.Format("01-02 15:04")+" 更新")
	}
	if st.took > 0 {
		parts = append(parts, "耗时 "+formatElapsed(st.took))
	}
	if !st.complete {
		parts = append(parts, "未完整")
	}
	return strings.Join(parts, " · ")
}
