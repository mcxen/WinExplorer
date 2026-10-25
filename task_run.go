package main

import (
	"context"
	"path/filepath"

	"github.com/egoist/mygo"

	"sunvale/internal/tasks"
)

// taskAsync reports whether runTask queues on the manager; headless tests
// (no app loop) run tasks inline so they see results at once. A var so a
// test can exercise the queued path.
var taskAsync = func() bool { return mygo.App.IsReady() }

// runTask runs fn as a task under spec: queued behind the per-disk slots
// once the app runs, inline otherwise. Retry reruns fn as is, so fn must
// work from its captured inputs alone.
func runTask(spec tasks.Spec, fn tasks.Runner) string {
	m := taskManager()
	if taskAsync() {
		return m.Submit(spec, fn)
	}
	r := m.Begin(spec)
	m.End(r, fn(r))
	return r.ID()
}

// runTaskWait runs a task and blocks until it finishes (AI tool calls,
// which answer the model with the outcome but still wait their turn).
func runTaskWait(ctx context.Context, spec tasks.Spec, fn tasks.Runner) (tasks.Snapshot, error) {
	m := taskManager()
	var runErr error
	id := runTask(spec, func(r *tasks.Reporter) error { runErr = fn(r); return runErr })
	s, err := m.Wait(ctx, id)
	if err != nil {
		return s, err
	}
	return s, runErr
}

// archiveSpec is the slot and labels of one compress/extract run.
func archiveSpec(kind, path, origin string) tasks.Spec {
	k := tasks.KindExtract
	if kind == "compress" {
		k = tasks.KindCompress
	}
	return tasks.Spec{
		Kind: k, Title: archiveVerb(kind) + " " + filepath.Base(path), Source: tasks.Source(origin),
		Path: path, IO: "disk", Volume: archiveVolume(path),
	}
}

// archiveRunner wraps fn so the run is findable by path (row highlight,
// cancel from the status bar) while it holds its slot.
func archiveRunner(kind, path string, fn func(t *archiveTask) error) tasks.Runner {
	return func(r *tasks.Reporter) error {
		t := &archiveTask{r: r, Kind: kind, Path: path}
		archiveTasks.Lock()
		if archiveTasks.live == nil {
			archiveTasks.live = map[string]*archiveTask{}
		}
		archiveTasks.live[filepath.Clean(path)] = t
		archiveTasks.Unlock()
		defer func() {
			r.ItemDone()
			archiveTasks.Lock()
			if archiveTasks.live[filepath.Clean(t.Path)] == t {
				delete(archiveTasks.live, filepath.Clean(t.Path))
			}
			archiveTasks.Unlock()
		}()
		return fn(t)
	}
}

// rebind moves a live run to a new path (a retried compress that had to
// pick a fresh name).
func (t *archiveTask) rebind(path string) {
	archiveTasks.Lock()
	if archiveTasks.live[filepath.Clean(t.Path)] == t {
		delete(archiveTasks.live, filepath.Clean(t.Path))
	}
	t.Path = path
	archiveTasks.live[filepath.Clean(path)] = t
	archiveTasks.Unlock()
}

// queueArchive runs one compress/extract as a queued task.
func queueArchive(kind, path, origin string, fn func(t *archiveTask) error) string {
	return runTask(archiveSpec(kind, path, origin), archiveRunner(kind, path, fn))
}
