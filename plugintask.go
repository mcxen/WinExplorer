package main

import (
	"sync"
	"time"
)

// plugintask.go is the plugin runtime's base plumbing: the asynchronous
// discipline every background plugin needs, written once instead of per
// plugin. A plugin declares a taskRunner for its requests and keeps only
// its own scan; the runner serializes the work — one task at a time, a
// request that arrives mid-run replaces the queued one and runs right
// after — repaints while a task runs so status lines can tick, and owns
// its goroutines' lifecycle so nothing leaks when the work ends. The
// per-window appFlag and appVal give dialogs the same keyed-by-App
// lifetime: a second window (Ctrl+N) never gets a dialog it did not ask
// for, and a closed window's leftover entry is harmless.

// taskRunner serializes one plugin's background tasks over T, its request
// type. Zero for tick means no progress repaints; a positive tick is how
// often the runner repaints while a task runs, for live status counts.
// The runner is safe from any goroutine.
type taskRunner[T any] struct {
	run  func(req T)
	tick time.Duration

	mu      sync.Mutex
	running bool
	pending *T
}

// submit starts req, or — while a task runs — replaces the request queued
// behind it, the newest one winning, as a rescan for a new folder should.
func (t *taskRunner[T]) submit(req T) {
	t.mu.Lock()
	if t.running {
		r := req
		t.pending = &r
		t.mu.Unlock()
		return
	}
	t.running = true
	t.mu.Unlock()
	go t.loop(req)
}

// loop runs one task, repaints, then the request queued behind it, until
// the queue is empty. The task itself runs on its own goroutine so the
// progress ticker repaints while it works; both end before the chain
// returns, so an idle runner holds no goroutines.
func (t *taskRunner[T]) loop(req T) {
	for {
		done := make(chan struct{})
		go func() {
			t.run(req)
			close(done)
		}()
		if t.tick > 0 {
			tick := time.NewTicker(t.tick)
		watch:
			for {
				select {
				case <-done:
					break watch
				case <-tick.C:
					repaint()
				}
			}
			tick.Stop()
		} else {
			<-done
		}
		repaint()
		t.mu.Lock()
		if t.pending == nil {
			t.running = false
			t.mu.Unlock()
			return
		}
		req, t.pending = *t.pending, nil
		t.mu.Unlock()
	}
}

// busy reports whether a task is running, for the status lines.
func (t *taskRunner[T]) busy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// appFlag is a per-window bool a modal flips. ui.Modal wants a stable
// *bool, so the flag is allocated once per window and returned after
// that; a second window never shares the first one's dialog.
type appFlag struct {
	mu sync.Mutex
	m  map[*App]*bool
}

// get returns the window's flag, allocating it on first use. The map
// itself comes up lazily: the flags are zero-value globals, so a nil map
// must not panic the first caller.
func (f *appFlag) get(a *App) *bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[*App]*bool{}
	}
	if b, ok := f.m[a]; ok {
		return b
	}
	b := new(bool)
	f.m[a] = b
	return b
}

// appVal is a per-window value of any type — the same keyed-by-App
// lifetime appFlag gives a bool, for dialog state like a treemap's drill
// position.
type appVal[T any] struct {
	mu sync.Mutex
	m  map[*App]T
}

func (v *appVal[T]) get(a *App) T {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.m[a]
}

func (v *appVal[T]) set(a *App, val T) {
	v.mu.Lock()
	if v.m == nil {
		v.m = map[*App]T{}
	}
	v.m[a] = val
	v.mu.Unlock()
}
