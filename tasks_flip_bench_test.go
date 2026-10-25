package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/tasks"
)

// flipBenchFixture is a busy middle area: 2000 files in front, 500
// finished tasks in history behind.
func flipBenchFixture(tb testing.TB) (*App, *ui.Tester) {
	tb.Helper()
	archiveTasks.Lock()
	archiveTasks.m, archiveTasks.live = nil, nil
	archiveTasks.Unlock()
	dir := tb.TempDir()
	for i := 0; i < 2000; i++ {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("文件-%04d.txt", i)), nil, 0o644)
	}
	m := taskManager()
	for i := 0; i < 500; i++ {
		r := m.Begin(tasks.Spec{Kind: tasks.KindCopy, Title: fmt.Sprintf("复制 %d", i), IO: "disk", Volume: "/b"})
		r.SetTotal(tasks.UnitBytes, 100, 1)
		m.End(r, nil)
	}
	a := newTestApp(tb.(*testing.T), dir)
	a.store = &persistedState{Pinned: defaultPins()}
	a.homePath = dir
	a.tasksTab = 3
	return a, ui.NewTester(a.view, 1280, 800)
}

// flipFrameTimes runs one flip over and back and returns each animated
// frame's build time.
func flipFrameTimes(a *App, tt *ui.Tester) []time.Duration {
	var out []time.Duration
	for _, back := range []bool{true, false} {
		a.tasksFlipped = back
		for i := 0; i < int(flipDuration/(16*time.Millisecond))+2; i++ {
			t0 := time.Now()
			tt.Frame()
			out = append(out, time.Since(t0))
			tt.Advance(16 * time.Millisecond)
		}
	}
	return out
}

// TestFlipFrameTimes logs p50 / p95 / max of the flip's frames (FLIP-PERF);
// run with -run FlipFrameTimes -v.
func TestFlipFrameTimes(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	for _, v := range []struct {
		name           string
		adaptive, blur bool
	}{{"3d-noblur", false, false}, {"3d-blur", false, true}, {"adaptive", true, true}} {
		flipAdaptive, flipBlurOn = v.adaptive, v.blur
		t.Run(v.name, flipTimes)
	}
	flipAdaptive, flipBlurOn = true, true
}

func flipTimes(t *testing.T) {
	a, tt := flipBenchFixture(t)
	tt.Frame()
	flipFrameTimes(a, tt) // warm
	var all []time.Duration
	for i := 0; i < 5; i++ {
		all = append(all, flipFrameTimes(a, tt)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	t.Logf("flip frames n=%d p50=%v p95=%v max=%v", len(all), all[len(all)/2], all[len(all)*95/100], all[len(all)-1])
}

// Mid-flip the front's listing is frozen and the back builds few cards;
// a slow frame drops to the crossfade.
func TestFlipFreezesAndFallsBack(t *testing.T) {
	a, tt := tasksViewFixture(t)
	tt.Frame()
	a.toggleTasksView()
	tt.Frame()
	tt.Advance(flipDuration / 5)
	tt.Frame()
	if a.flip == nil || a.flip.lists == nil {
		t.Fatal("no pre-warmed flip state")
	}
	n := len(a.visibleEntries())
	a.cur().entries = nil
	if len(a.visibleEntries()) != n {
		t.Fatal("front listing not frozen mid-flip")
	}
	a.flip.last = time.Now().Add(-time.Second)
	a.flipFrame(time.Now())
	if !a.flipLowEnd {
		t.Fatal("slow frame did not fall back")
	}
	tt.Advance(flipDuration)
	tt.Frame()
	if a.flip != nil {
		t.Fatal("flip state outlived the turn")
	}
}
