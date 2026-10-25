package tasks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DefaultSlots are the concurrent runs per slot key: an IO class, split
// per Volume for the disk classes.
var DefaultSlots = map[string]int{"disk": 2, "remote": 2, "cpu": 2, "net": 4}

// Options configures a Manager.
type Options struct {
	// HistoryPath is the JSON file finished tasks persist to; "" keeps
	// history in memory.
	HistoryPath string
	// Slots overrides DefaultSlots per IO class.
	Slots map[string]int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Throttle is the least time between two progress events of one task.
	Throttle time.Duration
}

// EventType says what changed.
type EventType int

const (
	EventAdded EventType = iota
	EventState
	EventProgress
	EventRemoved
)

// Event names a task that changed.
type Event struct {
	ID   string
	Type EventType
}

// Manager owns the tasks.
type Manager struct {
	opt Options

	mu      sync.Mutex
	seq     int
	tasks   []*task // live and finished this session, oldest first
	running map[string]int
	history []Snapshot // loaded from disk, oldest first
	subs    map[int]func(Event)
	subSeq  int
}

// NewManager makes a Manager and loads its history.
func NewManager(opt Options) *Manager {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Throttle == 0 {
		opt.Throttle = 150 * time.Millisecond
	}
	m := &Manager{opt: opt, running: map[string]int{}, subs: map[int]func(Event){}}
	m.history = loadHistory(opt.HistoryPath, opt.Now())
	return m
}

func (m *Manager) now() time.Time { return m.opt.Now() }

func (m *Manager) slotLimit(io string) int {
	if n, ok := m.opt.Slots[io]; ok {
		return n
	}
	if n, ok := DefaultSlots[io]; ok {
		return n
	}
	return 2
}

func slotKey(s Spec) string {
	if s.IO == "" {
		return ""
	}
	return s.IO + ":" + s.Volume
}

func (m *Manager) newTask(spec Spec, run Runner) *task {
	m.seq++
	now := m.now()
	ctx, cancel := context.WithCancel(context.Background())
	t := &task{m: m, spec: spec, run: run, ctx: ctx, cancel: cancel, done: make(chan struct{}), slot: slotKey(spec)}
	t.snap = Snapshot{
		ID:   fmt.Sprintf("%s-%d", now.Format("20060102T150405"), m.seq),
		Kind: spec.Kind, Title: spec.Title, Source: spec.Source, Origin: spec.Origin, Path: spec.Path,
		State: Queued, Created: now,
	}
	if t.snap.Source == "" {
		t.snap.Source = SourceUser
	}
	m.tasks = append(m.tasks, t)
	return t
}

// Submit queues run as a new task and returns its id. It starts as soon
// as its slot frees, higher Priority first, then in order.
func (m *Manager) Submit(spec Spec, run Runner) string {
	m.mu.Lock()
	t := m.newTask(spec, run)
	m.mu.Unlock()
	m.emit(Event{ID: t.snap.ID, Type: EventAdded})
	m.schedule()
	return t.snap.ID
}

// Begin registers a task the caller runs itself, already running, and
// returns its Reporter; End finishes it. It takes a slot when one is free
// but never waits for one.
func (m *Manager) Begin(spec Spec) *Reporter {
	m.mu.Lock()
	t := m.newTask(spec, nil)
	t.snap.State, t.snap.Started = Running, m.now()
	if t.slot != "" {
		m.running[t.slot]++
	}
	m.mu.Unlock()
	m.emit(Event{ID: t.snap.ID, Type: EventAdded})
	return &Reporter{t}
}

// End finishes a Begin task with err (nil for success).
func (m *Manager) End(r *Reporter, err error) {
	if r == nil {
		return
	}
	m.finish(r.t, err)
}

// schedule starts what the free slots allow.
func (m *Manager) schedule() {
	m.mu.Lock()
	var queued []*task
	for _, t := range m.tasks {
		t.mu.Lock()
		q := t.snap.State == Queued
		t.mu.Unlock()
		if q {
			queued = append(queued, t)
		}
	}
	sort.SliceStable(queued, func(i, j int) bool { return queued[i].spec.Priority > queued[j].spec.Priority })
	var start []*task
	for _, t := range queued {
		if t.slot != "" && m.running[t.slot] >= m.slotLimit(t.spec.IO) {
			continue
		}
		t.mu.Lock()
		if t.snap.State != Queued {
			t.mu.Unlock()
			continue
		}
		if t.slot != "" {
			m.running[t.slot]++
		}
		t.snap.State, t.snap.Started = Running, m.now()
		t.mu.Unlock()
		start = append(start, t)
	}
	m.mu.Unlock()
	for _, t := range start {
		m.emit(Event{ID: t.snap.ID, Type: EventState})
		go m.exec(t)
	}
}

func (m *Manager) exec(t *task) {
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("任务异常：%v", p)
			}
		}()
		err = t.run(&Reporter{t})
	}()
	m.finish(t, err)
}

func (m *Manager) finish(t *task, err error) {
	now := m.now()
	t.mu.Lock()
	if t.snap.State.Finished() {
		t.mu.Unlock()
		return
	}
	switch {
	case t.ctx.Err() != nil || errors.Is(err, ErrCanceled) || errors.Is(err, context.Canceled):
		t.snap.State, t.snap.Err = Canceled, ErrCanceled.Error()
	case err != nil:
		t.snap.State, t.snap.Err = Failed, err.Error()
	case t.snap.FailedCount > 0:
		t.snap.State = Partial
	default:
		t.snap.State = Done
		if t.snap.Total > 0 {
			t.snap.Done = t.snap.Total
		}
	}
	t.snap.Ended = now
	t.snap.Current, t.snap.CurDone, t.snap.CurTotal = "", 0, 0
	if t.paused {
		t.paused = false
		close(t.resume)
	}
	snap := t.snapshot0()
	t.mu.Unlock()
	t.cancel()
	close(t.done)
	m.mu.Lock()
	if t.slot != "" && m.running[t.slot] > 0 {
		m.running[t.slot]--
	}
	m.history = trimHistory(append(m.history, snap), now)
	hist := append([]Snapshot(nil), m.history...)
	m.mu.Unlock()
	saveHistory(m.opt.HistoryPath, hist)
	m.emit(Event{ID: snap.ID, Type: EventState})
	m.schedule()
}

// snapshot0 copies the record without speed math; t.mu held.
func (t *task) snapshot0() Snapshot {
	s := t.snap
	s.Recent = append([]Item(nil), t.snap.Recent...)
	s.Failed = append([]Item(nil), t.snap.Failed...)
	return s
}

func (m *Manager) find(id string) *task {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.snap.ID == id {
			return t
		}
	}
	return nil
}

// ErrNoTask is returned for an unknown id or a move the state forbids.
var ErrNoTask = errors.New("没有该任务")
var ErrState = errors.New("任务状态不允许该操作")

// Pause holds a running task at its next Wait.
func (m *Manager) Pause(id string) error {
	t := m.find(id)
	if t == nil {
		return ErrNoTask
	}
	t.mu.Lock()
	if t.snap.State != Running {
		t.mu.Unlock()
		return ErrState
	}
	t.paused, t.resume, t.snap.State = true, make(chan struct{}), Paused
	t.mu.Unlock()
	m.emit(Event{ID: id, Type: EventState})
	return nil
}

// Resume lets a paused task go on.
func (m *Manager) Resume(id string) error {
	t := m.find(id)
	if t == nil {
		return ErrNoTask
	}
	t.mu.Lock()
	if t.snap.State != Paused {
		t.mu.Unlock()
		return ErrState
	}
	t.paused, t.snap.State = false, Running
	t.samples = nil
	close(t.resume)
	t.mu.Unlock()
	m.emit(Event{ID: id, Type: EventState})
	return nil
}

// Cancel stops a task; a queued one ends at once.
func (m *Manager) Cancel(id string) error {
	t := m.find(id)
	if t == nil {
		return ErrNoTask
	}
	t.mu.Lock()
	st := t.snap.State
	t.mu.Unlock()
	if st.Finished() {
		return ErrState
	}
	t.cancel()
	if st == Queued {
		m.finish(t, ErrCanceled)
	}
	return nil
}

// Retry queues a failed, partial or canceled task's runner again as a
// new task and returns its id.
func (m *Manager) Retry(id string) (string, error) {
	t := m.find(id)
	if t == nil {
		return "", ErrNoTask
	}
	t.mu.Lock()
	st := t.snap.State
	t.mu.Unlock()
	if t.run == nil || !(st == Failed || st == Partial || st == Canceled) {
		return "", ErrState
	}
	return m.Submit(t.spec, t.run), nil
}

// Wait blocks until the task ends or ctx is done, and returns its snapshot.
func (m *Manager) Wait(ctx context.Context, id string) (Snapshot, error) {
	t := m.find(id)
	if t == nil {
		return Snapshot{}, ErrNoTask
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return m.Get(id), ctx.Err()
	}
	return m.Get(id), nil
}

// Get is one task's snapshot (this session's, then history).
func (m *Manager) Get(id string) Snapshot {
	if t := m.find(id); t != nil {
		return t.snapshot(m.now())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.history {
		if s.ID == id {
			return s
		}
	}
	return Snapshot{}
}

// List is this session's tasks, oldest first.
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	list := append([]*task(nil), m.tasks...)
	m.mu.Unlock()
	now := m.now()
	out := make([]Snapshot, 0, len(list))
	for _, t := range list {
		out = append(out, t.snapshot(now))
	}
	return out
}

// History is the finished tasks, newest first, persisted ones included.
func (m *Manager) History() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Snapshot, len(m.history))
	for i, s := range m.history {
		out[len(out)-1-i] = s
	}
	return out
}

// ClearFinished drops this session's finished tasks from List (history
// keeps them).
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	keep := m.tasks[:0]
	for _, t := range m.tasks {
		t.mu.Lock()
		fin := t.snap.State.Finished()
		t.mu.Unlock()
		if !fin {
			keep = append(keep, t)
		}
	}
	m.tasks = keep
	m.mu.Unlock()
	m.emit(Event{Type: EventRemoved})
}

// PauseAll pauses every running task; ResumeAll resumes every paused one.
func (m *Manager) PauseAll() {
	for _, s := range m.List() {
		if s.State == Running {
			_ = m.Pause(s.ID)
		}
	}
}

func (m *Manager) ResumeAll() {
	for _, s := range m.List() {
		if s.State == Paused {
			_ = m.Resume(s.ID)
		}
	}
}

// Subscribe calls fn on every event until the returned func is called.
// fn runs on the reporting goroutine: keep it short (post a repaint).
func (m *Manager) Subscribe(fn func(Event)) func() {
	m.mu.Lock()
	m.subSeq++
	id := m.subSeq
	m.subs[id] = fn
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.subs, id)
		m.mu.Unlock()
	}
}

func (m *Manager) emit(e Event) {
	m.mu.Lock()
	fns := make([]func(Event), 0, len(m.subs))
	for _, fn := range m.subs {
		fns = append(fns, fn)
	}
	m.mu.Unlock()
	for _, fn := range fns {
		fn(e)
	}
}

// progressed emits a progress event at most once per Throttle per task.
func (m *Manager) progressed(t *task, now time.Time) {
	t.mu.Lock()
	if now.Sub(t.lastEmit) < m.opt.Throttle {
		t.mu.Unlock()
		return
	}
	t.lastEmit = now
	t.mu.Unlock()
	m.emit(Event{ID: t.snap.ID, Type: EventProgress})
}
