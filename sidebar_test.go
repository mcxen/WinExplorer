package main

import (
	"sync/atomic"
	"testing"
	"time"
)

// sidebar_test.go covers the navigation pane's caches: the volume list and
// the 最近位置 existence checks must not put I/O on the view thread.

// TestSidebarVolumesAreCached: the pane asks for 此电脑's volumes every
// frame, so a fresh cache must serve the whole view without another read.
func TestSidebarVolumesAreCached(t *testing.T) {
	// Let a background refresh from another test land first.
	for i := 0; i < 100; i++ {
		volumeCache.mu.Lock()
		loading := volumeCache.loading
		volumeCache.mu.Unlock()
		if !loading {
			break
		}
		time.Sleep(time.Millisecond)
	}
	oldRead := readVolumes
	var reads atomic.Int32
	readVolumes = func() []Place {
		reads.Add(1)
		return oldRead()
	}
	t.Cleanup(func() { readVolumes = oldRead })
	loadVolumesNow() // fresh, and outside the view
	before := reads.Load()

	a := &App{}
	enterView()
	for i := 0; i < 5; i++ {
		if got := a.thisPCPlaces(); len(got) == 0 {
			t.Fatal("the pane shows no volume")
		}
	}
	leaveView()
	if got := reads.Load(); got != before {
		t.Fatalf("the view read the volume list %d more times", got-before)
	}
}

// TestDirExistsDoesNotWaitInView: an unknown 最近位置 is answered
// optimistically and checked on a goroutine; the view must not wait for a
// stat that can hang on a dead share.
func TestDirExistsDoesNotWaitInView(t *testing.T) {
	const dir = "/sunvale-b2-no-such-dir"
	dirExistsMu.Lock()
	delete(dirExistsSeen, dir)
	dirExistsMu.Unlock()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	oldStat := dirExistsStat
	dirExistsStat = func(string) bool {
		entered <- struct{}{}
		<-release
		return false
	}
	t.Cleanup(func() { dirExistsStat = oldStat })

	enterView()
	got := make(chan bool, 1)
	go func() { got <- dirExists(dir) }()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("an unchecked row was dropped before its stat")
		}
	case <-time.After(time.Second):
		t.Fatal("dirExists waited for the stat in the view")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no background stat was started")
	}
	leaveView()
	close(release)
	for i := 0; i < 100; i++ {
		dirExistsMu.Lock()
		e := dirExistsSeen[dir]
		checking := e != nil && e.checking
		dirExistsMu.Unlock()
		if !checking {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the background stat did not finish")
}

// TestNavTreeHiddenToggleRefilters: the tree's children were filtered when
// they were read, so toggling 显示隐藏的项目 must drop them and let the new
// setting filter the cached names again.
func TestNavTreeHiddenToggleRefilters(t *testing.T) {
	a, _, dir := navTreeApp(t)
	a.navTree.open[dir] = true
	if got := namesOfEntries(a.navChildren(dir)); len(got) != 2 {
		t.Fatalf("children = %v, want the two visible folders", got)
	}
	a.toggleHidden()
	if got := namesOfEntries(a.navChildren(dir)); len(got) != 3 {
		t.Fatalf("children after showing hidden = %v, want three", got)
	}
}
