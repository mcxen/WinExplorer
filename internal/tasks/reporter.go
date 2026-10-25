package tasks

import (
	"context"
	"errors"
	"io"
)

// ErrCanceled is what a canceled task's operations return.
var ErrCanceled = errors.New("已取消")

// Runner is the operation behind a task. It reports through r and returns
// nil, an error, or ErrCanceled / r.Context().Err() after a cancel.
type Runner func(r *Reporter) error

// Reporter is the operation's handle on its task. A nil *Reporter is
// valid and does nothing, so operations run the same without a task.
type Reporter struct{ t *task }

// ID is the task's id, "" for a nil Reporter.
func (r *Reporter) ID() string {
	if r == nil {
		return ""
	}
	return r.t.snap.ID
}

// Context is done when the task is canceled.
func (r *Reporter) Context() context.Context {
	if r == nil {
		return context.Background()
	}
	return r.t.ctx
}

// Canceled reports a cancel request.
func (r *Reporter) Canceled() bool { return r != nil && r.t.ctx.Err() != nil }

// Wait blocks while the task is paused and returns ErrCanceled once it is
// canceled; operations call it between items (Reader calls it per read).
func (r *Reporter) Wait() error {
	if r == nil {
		return nil
	}
	for {
		r.t.mu.Lock()
		paused, ch := r.t.paused, r.t.resume
		r.t.mu.Unlock()
		if r.t.ctx.Err() != nil {
			return ErrCanceled
		}
		if !paused {
			return nil
		}
		select {
		case <-ch:
		case <-r.t.ctx.Done():
			return ErrCanceled
		}
	}
}

func (r *Reporter) update(fn func(s *Snapshot), progress bool) {
	if r == nil {
		return
	}
	now := r.t.m.now()
	r.t.mu.Lock()
	fn(&r.t.snap)
	if progress {
		r.t.sampleLocked(now)
	}
	r.t.mu.Unlock()
	r.t.m.progressed(r.t, now)
}

// SetTotal sets what the task counts and how much there is; a retry or
// a fresh attempt starts the count over.
func (r *Reporter) SetTotal(u Unit, total int64, items int) {
	r.update(func(s *Snapshot) {
		s.Unit, s.Total, s.Done, s.ItemsTotal, s.ItemsDone = u, total, 0, items, 0
		s.Recent, s.Failed, s.FailedCount = nil, nil, 0
	}, false)
}

// Add counts n units on the task and on the current item.
func (r *Reporter) Add(n int64) {
	if n <= 0 {
		return
	}
	r.update(func(s *Snapshot) { s.Done += n; s.CurDone += n }, true)
}

// AddTotal counts n units on the task only (compressed bytes read while
// the item counts what it writes).
func (r *Reporter) AddTotal(n int64) {
	if n <= 0 {
		return
	}
	r.update(func(s *Snapshot) { s.Done += n }, true)
}

// AddItem counts n units on the current item only.
func (r *Reporter) AddItem(n int64) {
	if n <= 0 {
		return
	}
	r.update(func(s *Snapshot) { s.CurDone += n }, true)
}

// Item starts a sub-item: its name and size (0 unknown).
func (r *Reporter) Item(name string, size int64) {
	r.update(func(s *Snapshot) { s.Current, s.CurDone, s.CurTotal = name, 0, size }, false)
}

// ItemDone files the current item as processed.
func (r *Reporter) ItemDone() {
	r.update(func(s *Snapshot) {
		if s.Current == "" {
			return
		}
		s.ItemsDone++
		s.Recent = append(s.Recent, Item{Name: s.Current, Size: max(s.CurTotal, s.CurDone)})
		if len(s.Recent) > recentKeep {
			s.Recent = s.Recent[len(s.Recent)-recentKeep:]
		}
		s.Current, s.CurDone, s.CurTotal = "", 0, 0
	}, false)
}

// Fail records a failed item and lets the task go on.
func (r *Reporter) Fail(name string, err error) {
	if err == nil {
		return
	}
	r.update(func(s *Snapshot) {
		s.FailedCount++
		if len(s.Failed) < failedKeep {
			s.Failed = append(s.Failed, Item{Name: name, Err: err.Error()})
		}
		if s.Current == name {
			s.Current, s.CurDone, s.CurTotal = "", 0, 0
		}
	}, false)
}

// Reader counts what flows through rd on the task and the current item,
// waiting while paused and stopping on cancel.
func (r *Reporter) Reader(rd io.Reader) io.Reader { return r.reader(rd, true, true) }

// TotalReader counts on the task only; ItemReader on the item only.
func (r *Reporter) TotalReader(rd io.Reader) io.Reader { return r.reader(rd, true, false) }
func (r *Reporter) ItemReader(rd io.Reader) io.Reader  { return r.reader(rd, false, true) }

func (r *Reporter) reader(rd io.Reader, total, item bool) io.Reader {
	if r == nil {
		return rd
	}
	return &countReader{rd, r, total, item}
}

type countReader struct {
	rd          io.Reader
	r           *Reporter
	total, item bool
}

func (c *countReader) Read(b []byte) (int, error) {
	if err := c.r.Wait(); err != nil {
		return 0, err
	}
	n, err := c.rd.Read(b)
	if n > 0 {
		k := int64(n)
		c.r.update(func(s *Snapshot) {
			if c.total {
				s.Done += k
			}
			if c.item {
				s.CurDone += k
			}
		}, true)
	}
	return n, err
}
