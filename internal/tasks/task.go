package tasks

import (
	"context"
	"sync"
	"time"
)

// Kind names what a task does; the values are stable JSON strings.
type Kind string

const (
	KindCompress Kind = "compress"
	KindExtract  Kind = "extract"
	KindCopy     Kind = "copy"
	KindMove     Kind = "move"
	KindDelete   Kind = "delete"
	KindRename   Kind = "rename"
	KindScript   Kind = "script"
)

// Source is who started a task.
type Source string

const (
	SourceUser     Source = "user"
	SourceAI       Source = "ai"
	SourceSchedule Source = "schedule"
	SourceRemote   Source = "remote"
)

// State is a task's place in its life.
type State int

const (
	Queued State = iota
	Running
	Paused
	Done
	Partial // done, with failed items
	Failed
	Canceled
)

var stateNames = [...]string{"queued", "running", "paused", "done", "partial", "failed", "canceled"}

func (s State) String() string {
	if int(s) < len(stateNames) {
		return stateNames[s]
	}
	return "unknown"
}

// Finished reports a terminal state.
func (s State) Finished() bool { return s >= Done }

// Unit is what Progress counts.
type Unit int

const (
	UnitNone  Unit = iota // indeterminate
	UnitBytes             // bytes
	UnitItems             // items
)

const (
	recentKeep = 50
	failedKeep = 200
	speedSpan  = 3 * time.Second
)

// Spec describes a task to start.
type Spec struct {
	Kind     Kind
	Title    string
	Source   Source
	Origin   string // assistant session or schedule id
	Priority int    // 1 foreground, 0 normal, -1 background
	// IO is the slot class; Volume splits disk classes per disk.
	IO     string
	Volume string
	// Path is the task's main path (the archive, the target folder).
	Path string
}

// Item is one processed or failed member.
type Item struct {
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
	Err  string `json:"err,omitempty"`
}

// Snapshot is a task's state at one moment, safe to keep and paint.
type Snapshot struct {
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Title  string `json:"title"`
	Source Source `json:"source"`
	Origin string `json:"origin,omitempty"`
	Path   string `json:"path,omitempty"`
	State  State  `json:"state"`
	Err    string `json:"err,omitempty"`

	Created time.Time `json:"created"`
	Started time.Time `json:"started,omitempty"`
	Ended   time.Time `json:"ended,omitempty"`

	Unit       Unit   `json:"unit"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	ItemsDone  int    `json:"itemsDone,omitempty"`
	ItemsTotal int    `json:"itemsTotal,omitempty"`
	Current    string `json:"-"`
	CurDone    int64  `json:"-"`
	CurTotal   int64  `json:"-"`

	Speed float64       `json:"-"` // units per second
	ETA   time.Duration `json:"-"`

	Recent      []Item `json:"recent,omitempty"`
	Failed      []Item `json:"failed,omitempty"`
	FailedCount int    `json:"failedCount,omitempty"`
}

// Frac is Done/Total in [0,1], or -1 when indeterminate.
func (s Snapshot) Frac() float64 {
	if s.Total <= 0 {
		if s.State == Done {
			return 1
		}
		return -1
	}
	return min(float64(s.Done)/float64(s.Total), 1)
}

// CurFrac is the current item's share, -1 when unknown.
func (s Snapshot) CurFrac() float64 {
	if s.CurTotal <= 0 {
		return -1
	}
	return min(float64(s.CurDone)/float64(s.CurTotal), 1)
}

// Elapsed is the run time so far (or in total once ended).
func (s Snapshot) Elapsed(now time.Time) time.Duration {
	if s.Started.IsZero() {
		return 0
	}
	if !s.Ended.IsZero() {
		return s.Ended.Sub(s.Started)
	}
	return now.Sub(s.Started)
}

type sample struct {
	at   time.Time
	done int64
}

// task is the live record behind a Snapshot.
type task struct {
	m    *Manager
	spec Spec
	run  Runner

	mu       sync.Mutex
	snap     Snapshot
	samples  []sample
	paused   bool
	resume   chan struct{} // closed on resume
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{} // closed when the run ends
	lastEmit time.Time
	slot     string
}

func (t *task) snapshot(now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.snap
	s.Recent = append([]Item(nil), t.snap.Recent...)
	s.Failed = append([]Item(nil), t.snap.Failed...)
	if s.State == Running && len(t.samples) > 0 {
		first := t.samples[0]
		if dt := now.Sub(first.at).Seconds(); dt > 0.2 {
			s.Speed = float64(s.Done-first.done) / dt
		}
		if s.Speed > 0 && s.Total > s.Done {
			s.ETA = time.Duration(float64(s.Total-s.Done) / s.Speed * float64(time.Second))
		}
	} else if s.State.Finished() {
		if el := s.Elapsed(now).Seconds(); el > 0 {
			s.Speed = float64(s.Done) / el
		}
	}
	return s
}

// sampleLocked records progress for the speed window.
func (t *task) sampleLocked(now time.Time) {
	if n := len(t.samples); n > 0 && now.Sub(t.samples[n-1].at) < 100*time.Millisecond {
		return
	}
	t.samples = append(t.samples, sample{now, t.snap.Done})
	cut := 0
	for cut < len(t.samples)-1 && now.Sub(t.samples[cut].at) > speedSpan {
		cut++
	}
	t.samples = t.samples[cut:]
}
