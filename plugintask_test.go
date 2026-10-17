package main

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestTaskRunnerSerializes checks the runner's discipline: one task at a
// time, the newest request winning the queue — 2 is replaced by 3 and
// never runs.
func TestTaskRunnerSerializes(t *testing.T) {
	var mu sync.Mutex
	var order []int
	release := make(chan struct{})
	r := &taskRunner[int]{run: func(req int) {
		if req == 1 {
			<-release // hold the first task so the queue can fill
		}
		mu.Lock()
		order = append(order, req)
		mu.Unlock()
	}}
	r.submit(1)
	if !r.busy() {
		t.Fatal("runner idle right after submit")
	}
	r.submit(2)
	r.submit(3)
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for r.busy() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if r.busy() {
		t.Fatal("runner still busy after the queue drained")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 1 || order[1] != 3 {
		t.Fatalf("order = %v, want [1 3]", order)
	}
}

// TestTaskRunnerTick ends a ticking task cleanly: the ticker's goroutine
// is stopped, and an idle runner holds none.
func TestTaskRunnerTick(t *testing.T) {
	done := make(chan struct{})
	r := &taskRunner[int]{run: func(int) { <-done }, tick: 2 * time.Millisecond}
	r.submit(1)
	time.Sleep(10 * time.Millisecond)
	if !r.busy() {
		t.Fatal("runner idle while its task sleeps")
	}
	close(done)
	deadline := time.Now().Add(2 * time.Second)
	for r.busy() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if r.busy() {
		t.Fatal("runner still busy after its task ended")
	}
}

// TestAppFlagStablePointer checks the flag is allocated once per window:
// the same *bool every get, a different one per App.
func TestAppFlagStablePointer(t *testing.T) {
	var f appFlag
	a, b := &App{}, &App{}
	if f.get(a) != f.get(a) {
		t.Fatal("get returned different flags for one App")
	}
	if f.get(a) == f.get(b) {
		t.Fatal("two Apps share one dialog flag")
	}
}

// TestAppValGetSet checks the per-window value round trip and the zero
// value an unset window reads.
func TestAppValGetSet(t *testing.T) {
	var v appVal[int]
	a, b := &App{}, &App{}
	if got := v.get(a); got != 0 {
		t.Fatalf("unset get = %d, want 0", got)
	}
	v.set(a, 42)
	if got := v.get(a); got != 42 {
		t.Fatalf("get = %d, want 42", got)
	}
	if got := v.get(b); got != 0 {
		t.Fatalf("other window's get = %d, want 0", got)
	}
	v.set(a, 7)
	if got := v.get(a); got != 7 {
		t.Fatalf("get after overwrite = %d, want 7", got)
	}
}
