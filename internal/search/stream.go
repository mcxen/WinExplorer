package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

// DefaultMaxResults bounds one live search. The cap keeps a broad query from
// turning into an unbounded result set and gives the caller a stable stopping
// point across every root.
const DefaultMaxResults = 5000

const (
	flushEvery = 150 * time.Millisecond
	walkers    = 8
)

// Options bounds a live search and controls which entries it reports.
type Options struct {
	Hidden   bool
	Deadline time.Time
	Max      int
	// Ctx cancels the walk early: a newer query or a closed view stops the
	// old walkers at once instead of letting them run to the deadline.
	Ctx context.Context
}

// Stream walks all roots concurrently and emits matching entries in batches.
// The final callback always arrives with more=false, including when the final
// batch is empty, so callers can distinguish a quiet end from a running walk.
func Stream(roots []string, query searchquery.Query, opts Options, emit func(batch []fsentry.Entry, more bool)) {
	max := opts.Max
	if max <= 0 {
		max = DefaultMaxResults
	}
	parent := opts.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithDeadline(parent, opts.Deadline)
	defer cancel()

	found := make(chan fsentry.Entry, 256)
	var (
		wg    sync.WaitGroup
		slots = make(chan struct{}, walkers)
		total atomic.Int64
	)
	for _, root := range roots {
		wg.Add(1)
		slots <- struct{}{}
		go func(root string) {
			defer wg.Done()
			defer func() { <-slots }()
			walkRoot(ctx, root, query, opts.Hidden, max, found, &total)
		}(root)
	}
	go func() {
		wg.Wait()
		close(found)
	}()

	var batch []fsentry.Entry
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-found:
			if !ok {
				emit(batch, false)
				return
			}
			batch = append(batch, e)
		case <-ticker.C:
			if len(batch) > 0 {
				// A fresh slice per batch: the receiver may keep it or hand
				// it to another goroutine, so it must not be overwritten.
				emit(batch, true)
				batch = nil
			}
		}
	}
}

// Walk collects one root's live search in a single result slice. Remote
// handlers and tests use it when streaming adds no value.
func Walk(root, rawQuery string, hidden bool, deadline time.Time) []fsentry.Entry {
	query := searchquery.Parse(rawQuery)
	var out []fsentry.Entry
	Stream([]string{root}, query, Options{Hidden: hidden, Deadline: deadline}, func(batch []fsentry.Entry, _ bool) {
		out = append(out, batch...)
	})
	return out
}

// walkRoot sends every matching entry below root to found. The result cap is
// shared by all roots so one fast tree cannot exceed the search's total limit.
func walkRoot(ctx context.Context, root string, query searchquery.Query, hidden bool, max int, found chan<- fsentry.Entry, total *atomic.Int64) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if ctx.Err() != nil || total.Load() >= int64(max) {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root {
			if d.IsDir() && pruneDir(path) {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && !hidden {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		isDir := d.IsDir()
		ext := ""
		if !isDir {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		}
		info := searchquery.MatchInfo{IsDir: isDir, Ext: ext, Size: -1}
		if !isDir {
			if dInfo, err := d.Info(); err == nil {
				info.Size = dInfo.Size()
				info.ModTime = dInfo.ModTime()
			}
		}
		if info.IsDir {
			info.Size = 0
		}
		if !query.Matches(name, path, info) {
			return nil
		}
		e := entryFrom(path, d)
		if e.Path == "" {
			return nil
		}
		if total.Add(1) > int64(max) {
			return filepath.SkipAll
		}
		select {
		case found <- e:
		case <-ctx.Done():
			return filepath.SkipAll
		}
		return nil
	})
}

// pruneDir keeps virtual filesystems and separately rooted mount points out
// of a volume walk. Walking them again is endless, duplicated, or both.
func pruneDir(path string) bool {
	for _, p := range platformPrune() {
		if path == p {
			return true
		}
	}
	return false
}

// PruneDir exposes the platform walk rule to callers that still keep a thin
// compatibility wrapper in the application package.
func PruneDir(path string) bool { return pruneDir(path) }

// entryFrom builds the shared row type for a walk hit, resolving symlinked
// directories so opening the hit behaves like opening the target folder.
func entryFrom(path string, d os.DirEntry) fsentry.Entry {
	isDir := d.IsDir()
	if !isDir && d.Type()&os.ModeSymlink != 0 {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			isDir = true
		}
	}
	e := fsentry.Entry{
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
