package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const indexVersion = 1

type indexDiskEntry struct {
	Path   string `json:"p"`
	Size   int64  `json:"s,omitempty"`
	Mod    int64  `json:"m,omitempty"`
	Dir    bool   `json:"d,omitempty"`
	Hidden bool   `json:"h,omitempty"`
}

type indexDiskFile struct {
	Version  int              `json:"version"`
	Built    string           `json:"built"`
	Complete bool             `json:"complete"`
	Estimate int              `json:"estimate,omitempty"`
	Entries  []indexDiskEntry `json:"entries"`
}

// load reads the cache into memory. Missing, unreadable, corrupt and foreign
// files are left as an empty index for the next build to replace.
func (ix *Index) load() {
	if ix.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(ix.cacheFile)
	if err != nil {
		return
	}
	var f indexDiskFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != indexVersion {
		return
	}
	items := make(map[string]indexEntry, len(f.Entries))
	for _, d := range f.Entries {
		if d.Path == "" {
			continue
		}
		items[d.Path] = indexEntry{path: d.Path, size: d.Size, mod: d.Mod, isDir: d.Dir, hidden: d.Hidden}
	}
	built, err := time.Parse(time.RFC3339, f.Built)
	if err != nil {
		built = time.Time{}
	}
	ix.mu.Lock()
	ix.items = items
	ix.built = built
	ix.complete = f.Complete
	ix.estimate = f.Estimate
	if ix.estimate <= 0 {
		ix.estimate = len(items)
	}
	ix.mu.Unlock()
}

// save writes a deterministic snapshot through a temporary file and rename.
func (ix *Index) save() {
	if ix.cacheFile == "" {
		return
	}
	ix.mu.RLock()
	entries := make([]indexDiskEntry, 0, len(ix.items))
	for _, e := range ix.items {
		entries = append(entries, indexDiskEntry{
			Path: e.path, Size: e.size, Mod: e.mod, Dir: e.isDir, Hidden: e.hidden,
		})
	}
	f := indexDiskFile{
		Version:  indexVersion,
		Built:    ix.built.Format(time.RFC3339),
		Complete: ix.complete,
		Estimate: ix.estimate,
	}
	ix.mu.RUnlock()
	slices.SortFunc(entries, func(a, b indexDiskEntry) int { return strings.Compare(a.Path, b.Path) })
	f.Entries = entries
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(ix.cacheFile), 0o755); err != nil {
		return
	}
	tmp := ix.cacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, ix.cacheFile)
}
