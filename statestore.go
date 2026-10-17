package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// persistedState is everything Sunvale keeps between runs, at
// os.UserConfigDir()/sunvale/state.json: the folders pinned to 快速访问
// (Explorer keeps them in the registry), the recently opened files the Home
// page lists (Explorer's 最近使用的文件), the delete log that lets the
// 回收站 page restore files to where they came from (Explorer keeps this in
// the recycle bin metadata), the remote access account (remote.go), the
// view settings the settings dialog edits (Explorer's folder options), and
// the built-in plugins the settings dialog's 插件 section has turned off —
// Plugins records only those, so a nil list is every plugin on, a fresh
// install's behavior.
type persistedState struct {
	mu sync.Mutex `json:"-"`

	Pinned   []string      `json:"pinned"`
	Recent   []string      `json:"recent"`
	TrashLog []trashRecord `json:"trash"`
	Remote   *remoteConfig `json:"remote"`
	Shares   []remoteShare `json:"shares"`
	Settings *appSettings  `json:"settings"`
	Plugins  []string      `json:"plugins"`
}

// appSettings is the view state the settings dialog edits: the 查看>显示
// toggles, 紧凑视图, the preview pane, and the current view mode (state.go's
// ViewMode).
type appSettings struct {
	ShowHidden     bool `json:"showHidden"`
	ShowExt        bool `json:"showExt"`
	Compact        bool `json:"compact"`
	ItemCheckboxes bool `json:"itemCheckboxes"`
	PreviewOpen    bool `json:"previewOpen"`
	Mode           int  `json:"mode"`
}

// defaultSettings are the settings a fresh app starts with, mirroring
// newAppWithStore in main.go: extensions shown, everything else off, and the
// details view.
func defaultSettings() *appSettings {
	return &appSettings{ShowExt: true, Mode: int(ViewDetails)}
}

// trashRecord maps a file in ~/.Trash back to the path it was deleted from.
type trashRecord struct {
	Original  string `json:"original"`
	TrashName string `json:"trash"`
}

const (
	stateDirName       = "sunvale"
	legacyStateDirName = "winexplorer"

	maxRecent   = 20
	maxUndoKeep = 64
)

// loadState reads the persisted state, seeding 快速访问 with the standard
// user folders on first run, as Explorer's first-run Quick Access does. It
// falls back to the directory the app used before the Sunvale rename, so pins
// and 最近使用的文件 survive the upgrade.
func loadState() *persistedState {
	st := &persistedState{}
	if dir, err := os.UserConfigDir(); err == nil {
		for _, name := range []string{stateDirName, legacyStateDirName} {
			data, err := os.ReadFile(filepath.Join(dir, name, "state.json"))
			if err != nil {
				continue
			}
			if err := json.Unmarshal(data, st); err != nil {
				st = &persistedState{}
				continue
			}
			if st.Settings == nil {
				st.Settings = defaultSettings()
			}
			break
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
// caller. Errors are ignored: a lost pin is harmless. Saving into the new
// directory is what completes the move away from the pre-rename one.
func (st *persistedState) saveLocked() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, stateDirName), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, stateDirName, "state.json"), data, 0o644)
}

// defaultPins are the folders Explorer pins on a fresh profile.
func defaultPins() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var out []string
	for _, name := range []string{"Desktop", "Downloads", "Documents", "Pictures", "Music", "Videos"} {
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

// setRemote persists the remote access configuration (remote.go).
func (st *persistedState) setRemote(rc *remoteConfig) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Remote = rc
	st.saveLocked()
}

// settings returns a copy of the view settings, the defaults before the
// first save.
func (st *persistedState) settings() appSettings {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.Settings == nil {
		return *defaultSettings()
	}
	return *st.Settings
}

// updateSettings applies fn to the view settings and saves, starting from
// the defaults on the first change.
func (st *persistedState) updateSettings(fn func(s *appSettings)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.Settings == nil {
		st.Settings = defaultSettings()
	}
	fn(st.Settings)
	st.saveLocked()
}

// pluginList returns a copy of the turned-off plugin ids, nil when every
// plugin is on.
func (st *persistedState) pluginList() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.Plugins...)
}

// setPlugin records the plugin as turned off, or drops the record when it
// is turned back on; only the off ids are kept, so the list is nil again
// once every plugin is on. A state that already holds the record skips the
// save, as pin does.
func (st *persistedState) setPlugin(id string, on bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	i := slices.Index(st.Plugins, id)
	if on {
		if i < 0 {
			return
		}
		st.Plugins = slices.Delete(st.Plugins, i, i+1)
		if len(st.Plugins) == 0 {
			st.Plugins = nil
		}
	} else {
		if i >= 0 {
			return
		}
		st.Plugins = append(st.Plugins, id)
	}
	st.saveLocked()
}

// shareAll returns a copy of the saved shares, newest first (remote_share.go).
func (st *persistedState) shareAll() []remoteShare {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]remoteShare(nil), st.Shares...)
}

// shareSaveAll replaces the saved shares.
func (st *persistedState) shareSaveAll(list []remoteShare) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Shares = append([]remoteShare(nil), list...)
	st.saveLocked()
}

// CompactPaths drops empties and duplicates in place.
func compactPaths(paths []string) []string {
	return slices.Compact(slices.DeleteFunc(paths, func(p string) bool {
		return strings.TrimSpace(p) == ""
	}))
}
