package search

import (
	"os"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"sunvale/internal/fsentry"
)

// watcher keeps fsnotify watches on the most recently listed folders, never
// a whole disk: inotify's per-user watch limit is a few thousand on many
// systems. An event re-lists the folder (debounced) into the overlay.
type watcher struct {
	ix *Index

	mu      sync.Mutex
	w       *fsnotify.Watcher
	dirs    []string // LRU, oldest first
	pending map[string]*time.Timer
}

const (
	maxWatches    = 64
	watchDebounce = 300 * time.Millisecond
)

func newWatcher(ix *Index) *watcher {
	return &watcher{ix: ix, pending: map[string]*time.Timer{}}
}

func (w *watcher) add(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if i := slices.Index(w.dirs, dir); i >= 0 {
		w.dirs = append(slices.Delete(w.dirs, i, i+1), dir)
		return
	}
	if w.w == nil {
		fw, err := fsnotify.NewWatcher()
		if err != nil {
			return // unsupported platform or limit reached: refresh covers it
		}
		w.w = fw
		go w.loop(fw)
	}
	if err := w.w.Add(dir); err != nil {
		return
	}
	w.dirs = append(w.dirs, dir)
	for len(w.dirs) > maxWatches {
		_ = w.w.Remove(w.dirs[0])
		w.dirs = w.dirs[1:]
	}
}

func (w *watcher) loop(fw *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			w.schedule(dirOf(ev.Name))
		case _, ok := <-fw.Errors:
			if !ok {
				return
			}
		}
	}
}

func dirOf(path string) string {
	for i := len(path) - 1; i > 0; i-- {
		if os.IsPathSeparator(path[i]) {
			return path[:i]
		}
	}
	return path
}

func (w *watcher) schedule(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !slices.Contains(w.dirs, dir) {
		return
	}
	if t, ok := w.pending[dir]; ok {
		t.Reset(watchDebounce)
		return
	}
	w.pending[dir] = time.AfterFunc(watchDebounce, func() {
		w.mu.Lock()
		delete(w.pending, dir)
		w.mu.Unlock()
		w.relist(dir)
	})
}

func (w *watcher) relist(dir string) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	entries := make([]fsentry.Entry, 0, len(des))
	for _, de := range des {
		entries = append(entries, entryFrom(joinPath(dir, de.Name()), de))
	}
	w.ix.Note(dir, entries)
}

func (w *watcher) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, t := range w.pending {
		t.Stop()
	}
	w.pending = map[string]*time.Timer{}
	if w.w != nil {
		_ = w.w.Close()
		w.w = nil
	}
	w.dirs = nil
}
