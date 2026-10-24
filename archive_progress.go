package main

import (
	"errors"
	"io"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// archive_progress.go is the byte-based progress of compress and extract
// runs (ARCHIVE-PROG): one archiveTask per run, keyed by the archive path,
// counted by wrapping the readers the zip/tar code copies through. The
// status bar and the assistant transcript paint snapshots of it.

var errArchiveCanceled = errors.New("已取消")

// archiveTaskKeep is how long a finished task stays listed.
const archiveTaskKeep = 10 * time.Minute

// archiveNow is the clock; tests swap it.
var archiveNow = time.Now

type archiveTask struct {
	ID     int64
	Kind   string // compress | extract
	Path   string // the archive (written or read)
	Origin string // user | ai
	Start  time.Time

	done     atomic.Int64
	total    atomic.Int64
	canceled atomic.Bool

	mu       sync.Mutex
	cur      string
	finished time.Time
	err      string
}

// archiveTaskSnap is one painted state of a task.
type archiveTaskSnap struct {
	ID                 int64
	Kind, Name, Origin string
	Cur                string
	Done, Total        int64
	Speed              float64 // bytes per second
	ETA                time.Duration
	Elapsed            time.Duration
	Finished, Canceled bool
	Err                string
	Start              time.Time
	// Title and Meta override the verb + name and the numbers line (an
	// OpenUI TaskProgress the model wrote).
	Title, Meta string
	Static      bool
}

// Frac is the done share in [0,1]; -1 when the total is unknown.
func (s archiveTaskSnap) Frac() float64 {
	if s.Total <= 0 {
		if s.Finished && s.Err == "" {
			return 1
		}
		return -1
	}
	f := float64(s.Done) / float64(s.Total)
	if f > 1 {
		f = 1
	}
	return f
}

var archiveTasks struct {
	sync.Mutex
	next int64
	list []*archiveTask
	// onChange repaints the window; nil in tests.
	onChange func()
}

// startArchiveTask registers a run; total may be 0 (unknown, grows later).
func startArchiveTask(kind, path, origin string, total int64) *archiveTask {
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	archiveTasks.next++
	t := &archiveTask{ID: archiveTasks.next, Kind: kind, Path: path, Origin: origin, Start: archiveNow()}
	t.total.Store(total)
	now := archiveNow()
	keep := archiveTasks.list[:0]
	for _, o := range archiveTasks.list {
		o.mu.Lock()
		old := !o.finished.IsZero() && now.Sub(o.finished) > archiveTaskKeep
		o.mu.Unlock()
		if !old {
			keep = append(keep, o)
		}
	}
	archiveTasks.list = append(keep, t)
	return t
}

// archiveTaskFor finds the running task reading or writing path.
func archiveTaskFor(path string) *archiveTask {
	path = filepath.Clean(path)
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	for i := len(archiveTasks.list) - 1; i >= 0; i-- {
		t := archiveTasks.list[i]
		if filepath.Clean(t.Path) == path {
			t.mu.Lock()
			live := t.finished.IsZero()
			t.mu.Unlock()
			if live {
				return t
			}
		}
	}
	return nil
}

func archiveTaskByID(id int64) *archiveTask {
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	for _, t := range archiveTasks.list {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func archiveChanged() {
	archiveTasks.Lock()
	fn := archiveTasks.onChange
	archiveTasks.Unlock()
	if fn != nil {
		fn()
	}
}

// reset starts a fresh attempt (a password retry re-reads the archive).
func (t *archiveTask) reset(total int64) {
	if t == nil {
		return
	}
	t.done.Store(0)
	if total > 0 {
		t.total.Store(total)
	}
}

func (t *archiveTask) setFile(name string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.cur = name
	t.mu.Unlock()
}

func (t *archiveTask) add(n int64) {
	if t != nil && n > 0 {
		t.done.Add(n)
	}
}

func (t *archiveTask) cancel() {
	if t != nil {
		t.canceled.Store(true)
		archiveChanged()
	}
}

func (t *archiveTask) isCanceled() bool { return t != nil && t.canceled.Load() }

// finish ends the run; err nil is success.
func (t *archiveTask) finish(err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.finished = archiveNow()
	if err != nil {
		t.err = err.Error()
		if t.canceled.Load() || errors.Is(err, errArchiveCanceled) {
			t.err = errArchiveCanceled.Error()
		}
	} else if tot := t.total.Load(); tot > 0 {
		t.done.Store(tot)
	}
	t.cur = ""
	t.mu.Unlock()
	archiveChanged()
}

func (t *archiveTask) snap() archiveTaskSnap {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := archiveTaskSnap{
		ID: t.ID, Kind: t.Kind, Name: filepath.Base(t.Path), Origin: t.Origin, Cur: t.cur,
		Done: t.done.Load(), Total: t.total.Load(), Start: t.Start,
		Finished: !t.finished.IsZero(), Canceled: t.canceled.Load(), Err: t.err,
	}
	end := archiveNow()
	if s.Finished {
		end = t.finished
	}
	s.Elapsed = end.Sub(t.Start)
	if sec := s.Elapsed.Seconds(); sec > 0.2 {
		s.Speed = float64(s.Done) / sec
	}
	if !s.Finished && s.Speed > 0 && s.Total > s.Done {
		s.ETA = time.Duration(float64(s.Total-s.Done) / s.Speed * float64(time.Second))
	}
	return s
}

// archiveSnaps lists tasks, oldest first, that keep(s) accepts.
func archiveSnaps(keep func(archiveTaskSnap) bool) []archiveTaskSnap {
	archiveTasks.Lock()
	list := append([]*archiveTask(nil), archiveTasks.list...)
	archiveTasks.Unlock()
	var out []archiveTaskSnap
	for _, t := range list {
		if s := t.snap(); keep == nil || keep(s) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// progressReader counts bytes through r into t and stops on cancel.
type progressReader struct {
	r io.Reader
	t *archiveTask
}

func (p progressReader) Read(b []byte) (int, error) {
	if p.t.isCanceled() {
		return 0, errArchiveCanceled
	}
	n, err := p.r.Read(b)
	p.t.add(int64(n))
	return n, err
}

// withProgress wraps r when t is set.
func withProgress(r io.Reader, t *archiveTask) io.Reader {
	if t == nil {
		return r
	}
	return progressReader{r, t}
}
