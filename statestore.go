package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// persistedState is everything WinExplorer keeps between runs, at
// os.UserConfigDir()/winexplorer/state.json: the folders pinned to 快速访问
// (Explorer keeps them in the registry), the recently opened files the Home
// page lists (Explorer's 最近使用的文件), and the delete log that lets the
// 回收站 page restore files to where they came from (Explorer keeps this in
// the recycle bin metadata).
type persistedState struct {
	mu sync.Mutex `json:"-"`

	Pinned   []string      `json:"pinned"`
	Recent   []string      `json:"recent"`
	TrashLog []trashRecord `json:"trash"`
}

// trashRecord maps a file in ~/.Trash back to the path it was deleted from.
type trashRecord struct {
	Original  string `json:"original"`
	TrashName string `json:"trash"`
}

const (
	maxRecent   = 20
	maxUndoKeep = 64
)

// loadState reads the persisted state, seeding 快速访问 with the standard
// user folders on first run, as Explorer's first-run Quick Access does.
func loadState() *persistedState {
	st := &persistedState{}
	if dir, err := os.UserConfigDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(dir, "winexplorer", "state.json")); err == nil {
			_ = json.Unmarshal(data, st)
		}
	}
	if len(st.Pinned) == 0 {
		st.Pinned = defaultPins()
	}
	st.Pinned = compactPaths(st.Pinned)
	st.Recent = compactPaths(st.Recent)
	return st
}

// saveLocked writes the state back, with the mutex already held by the
// caller. Errors are ignored: a lost pin is harmless.
func (st *persistedState) saveLocked() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "winexplorer"), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "winexplorer", "state.json"), data, 0o644)
}

// defaultPins are the folders Explorer pins on a fresh profile.
func defaultPins() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var out []string
	for _, name := range []string{"Desktop", "Downloads", "Documents", "Pictures"} {
		p := filepath.Join(home, name)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// Pin adds a folder to 快速访问, first, unless it is pinned already.
func (st *persistedState) pin(path string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	path = filepath.Clean(path)
	if slices.Contains(st.Pinned, path) {
		return false
	}
	st.Pinned = slices.Insert(st.Pinned, 0, path)
	st.saveLocked()
	return true
}

// Unpin removes a folder from 快速访问.
func (st *persistedState) unpin(path string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, p := range st.Pinned {
		if p == filepath.Clean(path) {
			st.Pinned = slices.Delete(st.Pinned, i, i+1)
			st.saveLocked()
			return true
		}
	}
	return false
}

// IsPinned reports whether the folder is pinned.
func (st *persistedState) isPinned(path string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return slices.Contains(st.Pinned, filepath.Clean(path))
}

// PushRecent records an opened file on the Home page's 最近使用的文件,
// newest first, deduplicated, capped at maxRecent.
func (st *persistedState) pushRecent(path string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	path = filepath.Clean(path)
	st.Recent = slices.DeleteFunc(st.Recent, func(p string) bool {
		return p == path
	})
	st.Recent = slices.Insert(st.Recent, 0, path)
	if len(st.Recent) > maxRecent {
		st.Recent = st.Recent[:maxRecent]
	}
	st.saveLocked()
}

// PinnedList returns a copy of the pinned folder paths, newest first.
func (st *persistedState) pinnedList() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.Pinned...)
}

// RecentList returns a copy of the recent file paths, newest first.
func (st *persistedState) recentList() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.Recent...)
}

// logTrash and restoreFromTrash keep the 回收站 page's 还原 working: the
// record maps the trash name to the original path. Records whose file is
// gone from the trash are dropped as the list is consulted.
func (st *persistedState) logTrash(original, trashName string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.TrashLog = slices.DeleteFunc(st.TrashLog, func(r trashRecord) bool {
		return r.TrashName == trashName
	})
	st.TrashLog = append(st.TrashLog, trashRecord{Original: original, TrashName: trashName})
	if len(st.TrashLog) > 512 {
		st.TrashLog = st.TrashLog[len(st.TrashLog)-512:]
	}
	st.saveLocked()
}

func (st *persistedState) originalForTrash(trashName string) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.TrashLog {
		if r.TrashName == trashName {
			return r.Original, true
		}
	}
	return "", false
}

func (st *persistedState) forgetTrash(trashName string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.TrashLog = slices.DeleteFunc(st.TrashLog, func(r trashRecord) bool {
		return r.TrashName == trashName
	})
	st.saveLocked()
}

// clearTrash drops the whole restore log, after 清空回收站.
func (st *persistedState) clearTrash() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.TrashLog = nil
	st.saveLocked()
}

// CompactPaths drops empties and duplicates in place.
func compactPaths(paths []string) []string {
	return slices.Compact(slices.DeleteFunc(paths, func(p string) bool {
		return strings.TrimSpace(p) == ""
	}))
}
