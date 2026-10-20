package main

import (
	"os"
	"path/filepath"
	"strings"

	"sunvale/internal/netfs"
)

// lister reads one directory into the entries the pane shows. It is the
// seam the listing work is built on: reload names a lister for the folder
// it is about to show and knows nothing else about where the bytes come
// from, so a future direct SMB reader, an archive-backed listing or a test
// double slots in beside localLister without touching the pane.
type lister interface {
	List(dir string) ([]Entry, error)
}

// localLister reads the OS filesystem. parallel says the folder's metadata
// is worth reading from several goroutines at once — true for a network
// mount, where every stat is a round trip and overlapping them is most of
// the win, and false for a local disk, where a queue of parallel seeks
// mostly makes a spinning drive thrash.
type localLister struct{ parallel bool }

// listWorkers caps the goroutines a parallel listing uses: enough to hide
// the round trips, few enough not to flood a NAS.
const listWorkers = 8

// List reads dir: one ReadDir for the names and types, then each entry's
// stat — the modification time every row needs (folder-size caching keys on
// it), the size of a file, and whether a symlink points at a folder.
func (l localLister) List(dir string) ([]Entry, error) {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(dirs))
	for i, d := range dirs {
		name := d.Name()
		e := Entry{Name: name, Path: filepath.Join(dir, name), IsDir: d.IsDir()}
		if !e.IsDir {
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		}
		entries[i] = e
	}
	// statAt fills one entry from its stat; an entry gone since the read is
	// marked empty and dropped at the end.
	statAt := func(i int) {
		e := &entries[i]
		info, err := os.Lstat(e.Path)
		if err != nil {
			e.Path = ""
			return
		}
		e.ModTime = info.ModTime()
		if !e.IsDir && info.Mode()&os.ModeSymlink != 0 {
			if st, err := os.Stat(e.Path); err == nil && st.IsDir() {
				e.IsDir = true
			}
		}
		if !e.IsDir {
			e.Size = info.Size()
		}
	}
	if l.parallel && len(entries) > 1 {
		parallelFor(len(entries), listWorkers, statAt)
	} else {
		for i := range entries {
			statAt(i)
		}
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Path != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// entryLister is the lister for a folder: the parallel reader on a network
// mount, the serial one on a local disk.
func entryLister(dir string) lister {
	return localLister{parallel: netfs.IsNetworkPath(dir)}
}
