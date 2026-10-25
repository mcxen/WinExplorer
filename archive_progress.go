package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sunvale/internal/tasks"
)

// archive_progress.go puts compress and extract runs on the task kernel
// (TASKSYS T2, internal/tasks): each run is a Begin task keyed by its
// archive path, counted by wrapping the readers the zip/tar code copies
// through — the whole run's bytes and the member being written — so the
// task view, the status bar and the assistant's cards paint one record.

var errArchiveCanceled = tasks.ErrCanceled

// archiveNow is the task clock; tests swap it.
var archiveNow = time.Now

// archiveTasks holds the window's task manager and the live archive runs.
var archiveTasks struct {
	sync.Mutex
	m    *tasks.Manager
	live map[string]*archiveTask // clean archive path → run
	// onChange repaints the window; nil in tests.
	onChange func()
}

// taskManager is the process-wide Manager, its history beside state.json
// (kept in memory under go test).
func taskManager() *tasks.Manager {
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	if archiveTasks.m == nil {
		archiveTasks.m = newTaskManager()
	}
	return archiveTasks.m
}

func newTaskManager() *tasks.Manager {
	opt := tasks.Options{Now: func() time.Time { return archiveNow() }}
	if dir, err := os.UserConfigDir(); err == nil && !testing.Testing() {
		opt.HistoryPath = filepath.Join(dir, stateDirName, "tasks.json")
	}
	m := tasks.NewManager(opt)
	m.Subscribe(func(tasks.Event) { archiveChanged() })
	return m
}

// archiveTask is one run's handle: the kernel's Reporter plus its path.
type archiveTask struct {
	r    *tasks.Reporter
	Kind string
	Path string
}

// archiveVolume is the disk slot key: the volume name on Windows, the
// first path element elsewhere.
func archiveVolume(path string) string {
	if v := filepath.VolumeName(path); v != "" {
		return strings.ToUpper(v)
	}
	parts := strings.SplitN(strings.TrimPrefix(filepath.ToSlash(path), "/"), "/", 2)
	return "/" + parts[0]
}

// startArchiveTask registers a run; total may be 0 (unknown, set later).
func startArchiveTask(kind, path, origin string, total int64) *archiveTask {
	m := taskManager()
	r := m.Begin(archiveSpec(kind, path, origin))
	t := &archiveTask{r: r, Kind: kind, Path: path}
	if total > 0 {
		r.SetTotal(tasks.UnitBytes, total, 0)
	}
	archiveTasks.Lock()
	if archiveTasks.live == nil {
		archiveTasks.live = map[string]*archiveTask{}
	}
	archiveTasks.live[filepath.Clean(path)] = t
	archiveTasks.Unlock()
	return t
}

// archiveTaskFor finds the running task reading or writing path.
func archiveTaskFor(path string) *archiveTask {
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	return archiveTasks.live[filepath.Clean(path)]
}

// archiveTaskByID wraps a task id for its controls.
func archiveTaskByID(id string) *archiveTask {
	archiveTasks.Lock()
	defer archiveTasks.Unlock()
	for _, t := range archiveTasks.live {
		if t.r.ID() == id {
			return t
		}
	}
	return &archiveTask{r: nil, Path: id}
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
func (t *archiveTask) reset(total int64) { t.resetItems(total, 0) }

func (t *archiveTask) resetItems(total int64, items int) {
	if t == nil {
		return
	}
	u := tasks.UnitBytes
	if total <= 0 {
		u = tasks.UnitNone
	}
	t.r.SetTotal(u, total, items)
}

// setItem moves on to member name of size bytes (0 unknown).
func (t *archiveTask) setItem(name string, size int64) {
	if t == nil {
		return
	}
	t.r.ItemDone()
	t.r.Item(name, size)
}

func (t *archiveTask) setFile(name string) { t.setItem(name, 0) }

func (t *archiveTask) add(n int64) {
	if t != nil {
		t.r.Add(n)
	}
}

func (t *archiveTask) cancel() {
	if t == nil {
		return
	}
	id := t.r.ID()
	if id == "" {
		id = t.Path
	}
	_ = taskManager().Cancel(id)
}

func (t *archiveTask) isCanceled() bool { return t != nil && t.r.Canceled() }

// wait holds a paused run and reports a cancel.
func (t *archiveTask) wait() error {
	if t == nil {
		return nil
	}
	return t.r.Wait()
}

// finish ends the run; err nil is success.
func (t *archiveTask) finish(err error) {
	if t == nil {
		return
	}
	t.r.ItemDone()
	taskManager().End(t.r, err)
	archiveTasks.Lock()
	if archiveTasks.live[filepath.Clean(t.Path)] == t {
		delete(archiveTasks.live, filepath.Clean(t.Path))
	}
	archiveTasks.Unlock()
}

func (t *archiveTask) snap() archiveTaskSnap {
	return snapFromTask(taskManager().Get(t.r.ID()))
}

// archiveTaskSnap is one painted state of a task.
type archiveTaskSnap struct {
	ID                    string
	Kind, Name, Origin    string
	Path                  string
	State                 tasks.State
	Cur                   string
	CurDone, CurTotal     int64
	Done, Total           int64
	ItemsDone, ItemsTotal int
	Speed                 float64 // bytes per second
	ETA                   time.Duration
	Elapsed               time.Duration
	Finished, Canceled    bool
	Err                   string
	Start                 time.Time
	Recent, Failed        []tasks.Item
	FailedCount           int
	// Title and Meta override the verb + name and the numbers line (an
	// OpenUI TaskProgress the model wrote).
	Title, Meta string
	Static      bool
	Ask         *tasks.Ask
}

func snapFromTask(s tasks.Snapshot) archiveTaskSnap {
	kind := string(s.Kind)
	out := archiveTaskSnap{
		ID: s.ID, Kind: kind, Name: filepath.Base(s.Path), Origin: string(s.Source), Path: s.Path,
		State: s.State, Cur: s.Current, CurDone: s.CurDone, CurTotal: s.CurTotal,
		Done: s.Done, Total: s.Total, ItemsDone: s.ItemsDone, ItemsTotal: s.ItemsTotal,
		Speed: s.Speed, ETA: s.ETA, Elapsed: s.Elapsed(archiveNow()),
		Finished: s.State.Finished(), Canceled: s.State == tasks.Canceled, Err: s.Err, Start: s.Created,
		Recent: s.Recent, Failed: s.Failed, FailedCount: s.FailedCount,
	}
	if s.Path == "" || (s.Kind != tasks.KindCompress && s.Kind != tasks.KindExtract) {
		out.Title = s.Title
		if s.Path != "" {
			out.Name = strings.TrimPrefix(s.Title, archiveVerb(kind)+" ")
		}
	}
	out.Ask = s.Ask
	if s.State == tasks.Partial && out.Err == "" {
		out.Err = itoa(s.FailedCount) + " 项失败"
	}
	return out
}

// Frac is the done share in [0,1]; -1 when the total is unknown.
func (s archiveTaskSnap) Frac() float64 {
	if s.Total <= 0 {
		if s.Finished && s.Err == "" {
			return 1
		}
		return -1
	}
	return min(float64(s.Done)/float64(s.Total), 1)
}

// CurFrac is the current member's share, -1 when unknown.
func (s archiveTaskSnap) CurFrac() float64 {
	if s.CurTotal <= 0 {
		return -1
	}
	return min(float64(s.CurDone)/float64(s.CurTotal), 1)
}

// archiveSnaps lists this session's tasks, oldest first, that keep(s)
// accepts.
func archiveSnaps(keep func(archiveTaskSnap) bool) []archiveTaskSnap {
	var out []archiveTaskSnap
	for _, t := range taskManager().List() {
		if s := snapFromTask(t); keep == nil || keep(s) {
			out = append(out, s)
		}
	}
	return out
}

// withProgress counts r on the run and its member, pausing and stopping
// with the task.
func withProgress(r io.Reader, t *archiveTask) io.Reader {
	if t == nil {
		return r
	}
	return t.r.Reader(r)
}

func withTotalProgress(r io.Reader, t *archiveTask) io.Reader {
	if t == nil {
		return r
	}
	return t.r.TotalReader(r)
}

func withItemProgress(r io.Reader, t *archiveTask) io.Reader {
	if t == nil {
		return r
	}
	return t.r.ItemReader(r)
}
