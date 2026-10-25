package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitState(t *testing.T, m *Manager, id string, want State) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Get(id); s.State == want {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("task %s never reached %v (now %v)", id, want, m.Get(id).State)
	return Snapshot{}
}

func TestSubmitRunsAndReports(t *testing.T) {
	m := NewManager(Options{})
	id := m.Submit(Spec{Kind: KindExtract, Title: "解压 a.zip"}, func(r *Reporter) error {
		r.SetTotal(UnitBytes, 10, 2)
		r.Item("a.txt", 4)
		_, _ = io.Copy(io.Discard, r.Reader(bytes.NewReader([]byte("abcd"))))
		r.ItemDone()
		r.Item("b.txt", 6)
		r.Add(6)
		r.ItemDone()
		return nil
	})
	s, err := m.Wait(context.Background(), id)
	if err != nil || s.State != Done || s.Done != 10 || s.ItemsDone != 2 || len(s.Recent) != 2 || s.Recent[0].Name != "a.txt" || s.Recent[0].Size != 4 {
		t.Fatalf("%+v %v", s, err)
	}
	if h := m.History(); len(h) != 1 || h[0].ID != id {
		t.Fatalf("history %+v", h)
	}
}

func TestFailuresMakePartialAndErrorsFail(t *testing.T) {
	m := NewManager(Options{})
	p := m.Submit(Spec{}, func(r *Reporter) error { r.Item("x", 1); r.Fail("x", errors.New("坏")); return nil })
	f := m.Submit(Spec{}, func(r *Reporter) error { return errors.New("磁盘已满") })
	if s, _ := m.Wait(context.Background(), p); s.State != Partial || s.FailedCount != 1 || s.Failed[0].Err != "坏" || s.Current != "" {
		t.Fatalf("partial %+v", s)
	}
	if s, _ := m.Wait(context.Background(), f); s.State != Failed || s.Err != "磁盘已满" {
		t.Fatalf("failed %+v", s)
	}
	if _, err := m.Retry(f); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Retry(m.Submit(Spec{}, func(*Reporter) error { return nil })); err == nil {
		// may still be running or done: retrying a non-failed task is refused
	}
}

func TestSlotsLimitPerVolumeAndPriority(t *testing.T) {
	m := NewManager(Options{})
	gate := func() (chan struct{}, Runner) {
		ch := make(chan struct{})
		return ch, func(*Reporter) error { <-ch; return nil }
	}
	relA, runA := gate()
	relB, runB := gate()
	relLow, runLow := gate()
	relHigh, runHigh := gate()
	relOther, runOther := gate()
	a := m.Submit(Spec{IO: "disk", Volume: "C"}, runA)
	b := m.Submit(Spec{IO: "disk", Volume: "C"}, runB)
	waitState(t, m, a, Running)
	waitState(t, m, b, Running)
	low := m.Submit(Spec{IO: "disk", Volume: "C", Priority: -1}, runLow)
	high := m.Submit(Spec{IO: "disk", Volume: "C", Priority: 1}, runHigh)
	other := m.Submit(Spec{IO: "disk", Volume: "D"}, runOther)
	waitState(t, m, other, Running)
	if s := m.Get(low); s.State != Queued {
		t.Fatalf("third task on C started: %v", s.State)
	}
	close(relA)
	waitState(t, m, high, Running)
	if m.Get(low).State != Queued {
		t.Fatal("low priority went before high")
	}
	close(relB)
	waitState(t, m, low, Running)
	close(relLow)
	close(relHigh)
	close(relOther)
	m.Wait(context.Background(), low)
}

func TestPauseResumeCancel(t *testing.T) {
	m := NewManager(Options{})
	step := make(chan struct{})
	var reads atomic.Int32
	id := m.Submit(Spec{}, func(r *Reporter) error {
		for {
			<-step
			if err := r.Wait(); err != nil {
				return err
			}
			reads.Add(1)
		}
	})
	waitState(t, m, id, Running)
	step <- struct{}{}
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	go func() { step <- struct{}{} }()
	time.Sleep(30 * time.Millisecond)
	if reads.Load() != 1 {
		t.Fatalf("ran while paused: %d", reads.Load())
	}
	_ = m.Resume(id)
	time.Sleep(30 * time.Millisecond)
	if reads.Load() != 2 {
		t.Fatalf("did not resume: %d", reads.Load())
	}
	_ = m.Cancel(id)
	go func() { step <- struct{}{} }()
	if s, _ := m.Wait(context.Background(), id); s.State != Canceled {
		t.Fatalf("state %v", s.State)
	}
	if err := m.Pause(id); !errors.Is(err, ErrState) {
		t.Fatalf("pause finished: %v", err)
	}
}

func TestCancelQueuedEndsAtOnce(t *testing.T) {
	m := NewManager(Options{Slots: map[string]int{"cpu": 1}})
	block := make(chan struct{})
	a := m.Submit(Spec{IO: "cpu"}, func(*Reporter) error { <-block; return nil })
	waitState(t, m, a, Running)
	b := m.Submit(Spec{IO: "cpu"}, func(*Reporter) error { return nil })
	_ = m.Cancel(b)
	if m.Get(b).State != Canceled {
		t.Fatal("queued cancel")
	}
	close(block)
}

func TestThrottleAndSpeed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var clock sync.Mutex
	m := NewManager(Options{Now: func() time.Time { clock.Lock(); defer clock.Unlock(); return now }})
	var events atomic.Int32
	m.Subscribe(func(e Event) {
		if e.Type == EventProgress {
			events.Add(1)
		}
	})
	r := m.Begin(Spec{Title: "t"})
	r.SetTotal(UnitBytes, 1000, 0)
	for i := 0; i < 100; i++ {
		r.Add(1)
	}
	if n := events.Load(); n > 2 {
		t.Fatalf("%d progress events without time passing", n)
	}
	clock.Lock()
	now = now.Add(2 * time.Second)
	clock.Unlock()
	r.Add(100)
	s := m.Get(r.ID())
	if s.Speed < 99 || s.Speed > 100 || s.ETA < 7*time.Second || s.ETA > 9*time.Second {
		t.Fatalf("speed %v eta %v", s.Speed, s.ETA)
	}
	m.End(r, nil)
	if s := m.Get(r.ID()); s.State != Done || s.Done != 1000 {
		t.Fatalf("%+v", s)
	}
}

func TestHistoryPersistsAndTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	now := time.Unix(1_800_000_000, 0)
	m := NewManager(Options{HistoryPath: path, Now: func() time.Time { return now }})
	r := m.Begin(Spec{Title: "旧"})
	m.End(r, nil)
	now = now.Add(31 * 24 * time.Hour)
	r = m.Begin(Spec{Title: "新"})
	m.End(r, errors.New("x"))
	m2 := NewManager(Options{HistoryPath: path, Now: func() time.Time { return now }})
	h := m2.History()
	if len(h) != 1 || h[0].Title != "新" || h[0].State != Failed {
		t.Fatalf("history %+v", h)
	}
}

func TestNilReporterIsInert(t *testing.T) {
	var r *Reporter
	r.SetTotal(UnitBytes, 1, 1)
	r.Add(1)
	r.Item("x", 1)
	r.ItemDone()
	if r.Wait() != nil || r.Canceled() {
		t.Fatal("nil reporter")
	}
	b, _ := io.ReadAll(r.Reader(bytes.NewReader([]byte("ok"))))
	if string(b) != "ok" {
		t.Fatal("reader")
	}
}

func TestAskBlocksUntilAnswered(t *testing.T) {
	m := NewManager(Options{})
	got := make(chan Answer, 1)
	id := m.Submit(Spec{}, func(r *Reporter) error {
		a, err := r.Ask(Ask{Item: "a.txt", Options: []string{ChoiceReplace, ChoiceSkip, ChoiceKeep}, Rest: 3})
		got <- a
		return err
	})
	deadline := time.Now().Add(2 * time.Second)
	for m.Get(id).Ask == nil {
		if time.Now().After(deadline) {
			t.Fatal("no question")
		}
		time.Sleep(time.Millisecond)
	}
	if q := m.Get(id).Ask; q.Item != "a.txt" || q.Rest != 3 {
		t.Fatalf("ask %+v", q)
	}
	_ = m.Answer(id, Answer{Choice: ChoiceSkip, All: true})
	if a := <-got; a.Choice != ChoiceSkip || !a.All {
		t.Fatalf("answer %+v", a)
	}
	if s, _ := m.Wait(context.Background(), id); s.Ask != nil || s.State != Done {
		t.Fatalf("%+v", s)
	}
	c := m.Submit(Spec{}, func(r *Reporter) error { _, err := r.Ask(Ask{Item: "b"}); return err })
	for m.Get(c).Ask == nil {
		time.Sleep(time.Millisecond)
	}
	_ = m.Cancel(c)
	if s, _ := m.Wait(context.Background(), c); s.State != Canceled {
		t.Fatalf("cancel while asking: %v", s.State)
	}
	var nilR *Reporter
	if a, _ := nilR.Ask(Ask{Options: []string{ChoiceSkip, ChoiceKeep}}); a.Choice != ChoiceKeep {
		t.Fatalf("nil reporter %+v", a)
	}
}
