package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// search.go is Explorer's search: typing in the box searches the current
// folder and its subfolders in the background, bounded so a huge tree
// cannot wedge the app, and the details view shows a 位置 column with the
// folder each hit came from.

const (
	searchMaxResults = 5000
	searchTimeout    = 8 * time.Second
)

// searchState lives on the tab: the query the last walk was for, its
// results, and whether a walk is in flight.
type searchState struct {
	query   string
	results []Entry
	busy    bool
}

// updateSearch is called once per frame from view: it starts a walk when
// the query has changed and no walk is in flight. Results are applied on
// the main thread; a walk for an older query is dropped.
func (a *App) updateSearch() {
	t := a.cur()
	q := strings.TrimSpace(t.query)
	if specialPath(t.path) {
		return
	}
	if q == t.search.query {
		return
	}
	if t.search.busy {
		return
	}
	if q == "" {
		t.search.query = ""
		t.search.results = nil
		return
	}
	t.search.query = q
	t.search.busy = true
	t.search.results = nil
	tab := t
	root := t.path
	hidden := a.showHidden
	deadline := time.Now().Add(searchTimeout)
	go func() {
		hits := walkMatches(root, q, hidden, deadline)
		mygo.RunOnMain(func() {
			tab.search.busy = false
			if strings.TrimSpace(tab.query) != q || !a.ownsTab(tab) {
				return // a newer query or the tab closed
			}
			tab.search.results = hits
			repaint()
		})
	}()
}

// ownsTab reports whether tab still belongs to the app's tab strip.
func (a *App) ownsTab(tab *tabState) bool {
	for _, t := range a.tabs {
		if t == tab {
			return true
		}
	}
	return false
}

// walkMatches collects every file or folder under root whose name
// contains query (case-insensitive), newest-first within the cap, with
// Dir set for the 位置 column. Hidden folders are skipped unless the user
// shows hidden items. The deadline caps a pathological tree.
func walkMatches(root, query string, hidden bool, deadline time.Time) []Entry {
	query = strings.ToLower(query)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	hits := make([]Entry, 0, 256)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || len(hits) >= searchMaxResults {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") && !hidden {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(strings.ToLower(name), query) {
			if e := searchEntry(path, d); e.Path != "" {
				hits = append(hits, e)
			}
		}
		return nil
	})
	return hits
}

// searchEntry builds an Entry for a hit, resolving symlinked dirs.
func searchEntry(path string, d os.DirEntry) Entry {
	isDir := d.IsDir()
	if !isDir && d.Type()&os.ModeSymlink != 0 {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			isDir = true
		}
	}
	e := Entry{
		Name:  d.Name(),
		Path:  path,
		IsDir: isDir,
		Dir:   filepath.Dir(path),
	}
	if info, err := d.Info(); err == nil {
		e.Size = info.Size()
		e.ModTime = info.ModTime()
	}
	if !e.IsDir {
		e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	}
	return e
}
