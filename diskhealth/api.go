package diskhealth

import (
	"sync"
	"time"
)

// pollInterval is how long a report is served from cache. The probes shell
// out to diskutil/system_profiler, so a frame that re-renders the page
// must not re-run them; F5 calls Refresh to force the next read.
const pollInterval = 3 * time.Second

// historyLen is how many throughput samples the page can plot: at the
// poll interval that is about ten minutes, enough for the chart to show a
// burst settling down.
const historyLen = 200

var cache struct {
	sync.Mutex
	at      time.Time
	report  Report
	prev    map[string]Disk // the previous probe, for throughput deltas
	prevAt  time.Time
	history map[string][]Sample // recent samples per disk, oldest first
	loading bool
}

// onUpdate is called after a background probe lands, so the UI can redraw
// without polling. SetOnUpdate installs it; the zero value means nobody is
// watching, which is how tests run the package.
var onUpdate func()

// SetOnUpdate installs the callback a landing probe runs. Passing nil
// removes it.
func SetOnUpdate(fn func()) {
	cache.Lock()
	onUpdate = fn
	cache.Unlock()
}

// notify tells the UI a probe landed.
func notify() {
	cache.Lock()
	fn := onUpdate
	cache.Unlock()
	if fn != nil {
		fn()
	}
}

// Read returns the current report, probing when the cache is older than
// pollInterval. A probe that finds nothing keeps the last report rather
// than blanking the page — an external enclosure timing out must not make
// the built-in disk disappear.
func Read() Report {
	cache.Lock()
	if !cache.at.IsZero() && time.Since(cache.at) < pollInterval {
		r := cache.report
		cache.Unlock()
		return r
	}
	cache.Unlock()
	return probeAndStore()
}

// Refresh forces the next Read to probe again.
func Refresh() {
	cache.Lock()
	cache.at = time.Time{}
	cache.Unlock()
}

// ReadAsync returns the cached report at once and probes in the background
// when it is stale, so the page draws immediately with what is known and
// fills in when the probe lands. It is what the page's first frame uses.
func ReadAsync() Report {
	cache.Lock()
	r := cache.report
	stale := cache.at.IsZero() || time.Since(cache.at) >= pollInterval
	busy := cache.loading
	cache.Unlock()
	if stale && !busy {
		probeInBackground()
	}
	return r
}

// Watch starts probing in the background every pollInterval until the
// returned stop function is called. The page calls it while it is on
// screen, so the throughput figures keep moving; a page that is not
// visible costs nothing.
func Watch() (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	// Probe at once, so the page's first visit is not a blank frame.
	probeInBackground()
	go func() {
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				probeInBackground()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// probeInBackground runs one probe off the caller's goroutine, unless one
// is already running.
func probeInBackground() {
	cache.Lock()
	if cache.loading {
		cache.Unlock()
		return
	}
	cache.loading = true
	cache.Unlock()
	go func() {
		probeAndStore()
		cache.Lock()
		cache.loading = false
		cache.Unlock()
		notify()
	}()
}

// Ready reports whether a probe has ever completed, so the page can tell
// "still reading" from "no disks".
func Ready() bool {
	cache.Lock()
	defer cache.Unlock()
	return !cache.at.IsZero()
}

// probeAndStore runs one probe under the cache lock, so concurrent frames
// share a single set of subprocess calls.
func probeAndStore() Report {
	cache.Lock()
	defer cache.Unlock()
	// Another caller may have probed while this one waited for the lock.
	if !cache.at.IsZero() && time.Since(cache.at) < pollInterval {
		return cache.report
	}
	r := probe()
	if len(r.Disks) == 0 {
		// Keep the last good report; say why nothing new came back.
		if len(cache.report.Disks) > 0 {
			return cache.report
		}
		cache.report = r
		cache.at = time.Now()
		return r
	}
	addThroughput(r.Disks, cache.prev, cache.prevAt, r.At)
	cache.prev = indexByID(r.Disks)
	cache.prevAt = r.At
	cache.history = appendHistory(cache.history, r.Disks, r.At)
	r.History = cache.history
	r.HistorySpan = historySpan(cache.history)
	cache.report = r
	cache.at = time.Now()
	return r
}

// addThroughput fills each disk's ReadBps/WriteBps from the change in its
// lifetime byte counters since the previous probe. A counter that went
// backwards (a remounted or reset drive) yields no figure rather than a
// negative one; a disk new to this probe has nothing to compare with.
func addThroughput(disks []Disk, prev map[string]Disk, prevAt, now time.Time) {
	if prevAt.IsZero() || len(prev) == 0 {
		return
	}
	secs := now.Sub(prevAt).Seconds()
	if secs <= 0 {
		return
	}
	for i := range disks {
		p, ok := prev[disks[i].ID]
		if !ok {
			continue
		}
		// Either counter moving is enough to know the sampling works.
		if disks[i].ReadBytes >= 0 || disks[i].WriteBytes >= 0 {
			disks[i].RateKnown = true
		}
		if d := disks[i].ReadBytes - p.ReadBytes; d >= 0 && disks[i].ReadBytes >= 0 {
			disks[i].ReadBps = float64(d) / secs
		}
		if d := disks[i].WriteBytes - p.WriteBytes; d >= 0 && disks[i].WriteBytes >= 0 {
			disks[i].WriteBps = float64(d) / secs
		}
	}
}

func indexByID(disks []Disk) map[string]Disk {
	m := make(map[string]Disk, len(disks))
	for _, d := range disks {
		m[d.ID] = d
	}
	return m
}

// appendHistory records this probe's throughput per disk, keeping the last
// historyLen samples. It returns a fresh map holding copied slices: the
// report it lands in is read by the UI without a lock, so the cache must
// never mutate a map it already handed out.
func appendHistory(prev map[string][]Sample, disks []Disk, at time.Time) map[string][]Sample {
	hist := make(map[string][]Sample, len(prev)+len(disks))
	for id, list := range prev {
		hist[id] = list
	}
	for _, d := range disks {
		if !d.RateKnown {
			continue
		}
		list := make([]Sample, 0, len(hist[d.ID])+1)
		list = append(list, hist[d.ID]...)
		list = append(list, Sample{At: at, Read: d.ReadBps, Write: d.WriteBps})
		if len(list) > historyLen {
			list = list[len(list)-historyLen:]
		}
		hist[d.ID] = list
	}
	return hist
}

// historySpan is the time the oldest sample covers, for the chart's caption.
func historySpan(hist map[string][]Sample) time.Duration {
	var span time.Duration
	for _, list := range hist {
		if len(list) < 2 {
			continue
		}
		if d := list[len(list)-1].At.Sub(list[0].At); d > span {
			span = d
		}
	}
	return span
}
