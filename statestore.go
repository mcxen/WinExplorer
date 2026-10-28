package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
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
	Visited  []visitRecord `json:"visited,omitempty"`
	Session  *sessionState `json:"session,omitempty"`
	TrashLog []trashRecord `json:"trash"`
	Remote   *remoteConfig `json:"remote"`
	// RemoteStorages are the remote server's mounts (remote_storage.go):
	// each maps a virtual path to a disk folder, and the one at "/" is what
	// the remote browser opens on.
	RemoteStorages []remoteStorage `json:"remoteStorages,omitempty"`
	// RemoteShowHidden is the remote pages' 显示隐藏的项目 switch
	// (remote_fs.go). Off by default, and stored apart from the desktop's
	// own ShowHidden: the phone and the window hide different things.
	RemoteShowHidden bool          `json:"remoteShowHidden,omitempty"`
	Shares           []remoteShare `json:"shares"`
	Settings         *appSettings  `json:"settings"`
	Plugins          []string      `json:"plugins"`
	// PluginSettings holds each plugin's own key-value preferences, by
	// plugin id. A state file written before this field existed
	// unmarshals to nil, and the setters initialize it lazily.
	PluginSettings map[string]map[string]string `json:"pluginSettings"`

	// Freq is the usage ledger behind the assistant's 常用文件: how often
	// and how recently each path was opened, visited, pinned, attached or
	// read, so the model can be told what the user actually works with.
	Freq map[string]pathFreq `json:"freq,omitempty"`

	// OpenWith is the 打开方式 registry's user half (openwith.go): each
	// extension mapped to the application 始终使用 picked for it, riding
	// above the OS's own default. A nil map is no override anywhere.
	OpenWith map[string]string `json:"openWith,omitempty"`

	// path is the state file the store reads and writes, set by loadState
	// and never serialized. A store built in memory — the tests' and the
	// remote plugin's — has none and keeps its state in memory, instead
	// of writing the user's file.
	path string `json:"-"`

	// sessionCleared marks a 清除会话记录 in this run: the quit's capture
	// then leaves the session empty, so what was cleared does not come back
	// on the next launch.
	sessionCleared bool
}

// appSettings is the view state the settings dialog edits: the 外观 mode
// (theme.go's appearanceLight/appearanceDark, "" for 跟随系统), the
// 查看>显示 toggles, 紧凑视图, the preview pane, the current view mode
// (state.go's ViewMode), and the details view's user-set column widths —
// zero keeps the default width.
type appSettings struct {
	Appearance string `json:"appearance,omitempty"`
	ShowHidden bool   `json:"showHidden"`
	// TreeExpand is 展开到打开的文件夹: navigating opens the sidebar tree's
	// ancestor chain down to the current folder. nil (never chosen) is off,
	// as in Windows Explorer; v0.3.5 and earlier defaulted to on.
	TreeExpand *bool `json:"treeExpand,omitempty"`
	// TreeNoExpand is the pre-0.3.6 switch for the same option (true = off).
	// Kept so an old explicit off still reads as off; new saves use TreeExpand.
	TreeNoExpand bool `json:"treeNoExpand,omitempty"`
	// VisualStyle is 视觉风格: "v2" (default for fresh installs), "" 经典 (visual.go).
	VisualStyle    string `json:"visualStyle,omitempty"`
	ShowExt        bool   `json:"showExt"`
	Compact        bool   `json:"compact"`
	ItemCheckboxes bool   `json:"itemCheckboxes"`
	PreviewOpen    bool   `json:"previewOpen"`
	Mode           int    `json:"mode"`
	// ProgressStyle is the assistant task cards' look: bar (default) or
	// gauge (PROG-STYLE).
	ProgressStyle string `json:"progressStyle,omitempty"`
	// RestoreSession puts the last run's windows and tabs back on launch
	// (session.go). It defaults to on, so it is the one setting stored as
	// its negation: turned off writes the field.
	NoRestoreSession bool `json:"noRestoreSession,omitempty"`

	// NoNetworkWarmup stops a network folder's first tiles from decoding
	// their pictures before the pointer reaches them (files.go's
	// tileWarmup). Off by default: the first screenful warms up, the rest of
	// a share waits to be looked at. These are stored as negations so an
	// older state file — which has neither field — reads as the new default.
	NoNetworkWarmup bool `json:"noNetworkWarmup,omitempty"`

	// The tray menu's disk section (tray.go): both default to on, so they
	// are stored as their negations for the same reason — a state file
	// written before the tray had switches reads as showing both.
	NoTrayDiskCap   bool `json:"noTrayDiskCap,omitempty"`
	NoTrayDiskSpeed bool `json:"noTrayDiskSpeed,omitempty"`

	// SearchHotkey is the global 快速搜索 accelerator, in Electron form
	// ("Ctrl+Shift+K", "Super+Space"). Empty means CmdOrCtrl+K, the
	// default, so a state file from before the shortcut was editable keeps
	// Ctrl+K / ⌘K.
	SearchHotkey string `json:"searchHotkey,omitempty"`

	// UpdateMirrors are the download prefixes the updater tries in order
	// (updatemirror.go): "https://tvv.tw/" style proxies and "direct" for
	// github.com itself, separated by commas or newlines. Empty means
	// defaultUpdateMirrors. A string keeps appSettings comparable.
	UpdateMirrors string `json:"updateMirrors,omitempty"`

	// LogLevel is the diagnostic log level (applog_glue.go): error, warn,
	// info or debug. Empty means info.
	LogLevel string `json:"logLevel,omitempty"`

	ColName float32 `json:"colName,omitempty"`
	ColDate float32 `json:"colDate,omitempty"`
	ColType float32 `json:"colType,omitempty"`
	ColLoc  float32 `json:"colLoc,omitempty"`
	ColSize float32 `json:"colSize,omitempty"`
}

// restoreSession reports whether a launch puts the last run's windows and
// tabs back.
func (s appSettings) restoreSession() bool { return !s.NoRestoreSession }

// networkWarmup reports whether a network folder's first tiles decode their
// pictures before the pointer reaches them.
func (s appSettings) networkWarmup() bool { return !s.NoNetworkWarmup }

// defaultSettings are the settings a fresh app starts with, mirroring
// newAppWithStore in main.go: the system's appearance, extensions shown,
// everything else off, and the details view.
func defaultSettings() *appSettings {
	return &appSettings{ShowExt: true, Mode: int(ViewDetails), VisualStyle: "v2"}
}

// trashRecord maps a file in ~/.Trash back to the path it was deleted from.
type trashRecord struct {
	Original  string `json:"original"`
	TrashName string `json:"trash"`
}

// visitRecord is one folder the user has been in, for the sidebar's 最近位置.
// At is unix milliseconds and orders the list, newest first; the record is
// what survives a restart when the tab's own in-memory history does not.
type visitRecord struct {
	Path string `json:"path"`
	At   int64  `json:"at"`
}

// maxVisited is how many folders 最近位置 remembers.
const maxVisited = 40

// sessionState is what the next launch puts back on screen: one record per
// window, in the order they were open, and the window the user was in. It
// is written when the app quits, and each window's close refreshes its own
// record meanwhile, so a crash keeps the last steady state rather than
// losing it.
type sessionState struct {
	Windows []windowSession `json:"windows,omitempty"`
	// Active is the index of the focused window, before the one-shot tab
	// and window restoration moved to another one.
	Active int `json:"active,omitempty"`
}

// windowSession is one window: where it sat, how big it was, and the tabs
// it showed. Tabs holds each tab's folder, in tab order, and Active the tab
// in front. A window whose saved folder is gone opens on 主文件夹.
type windowSession struct {
	X      int      `json:"x,omitempty"`
	Y      int      `json:"y,omitempty"`
	W      int      `json:"w,omitempty"`
	H      int      `json:"h,omitempty"`
	Tabs   []string `json:"tabs,omitempty"`
	Active int      `json:"active,omitempty"`
	// History is each tab's back/forward list, parallel to Tabs. Older
	// session files omit it and restore one entry per tab as before.
	History [][]string `json:"history,omitempty"`
	HistIdx []int      `json:"histIdx,omitempty"`
}

// session returns a copy of the saved session, nil when none is.
func (st *persistedState) session() *sessionState {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.Session == nil {
		return nil
	}
	out := *st.Session
	out.Windows = append([]windowSession(nil), st.Session.Windows...)
	for i := range out.Windows {
		out.Windows[i].Tabs = append([]string(nil), out.Windows[i].Tabs...)
		out.Windows[i].HistIdx = append([]int(nil), out.Windows[i].HistIdx...)
		out.Windows[i].History = make([][]string, len(out.Windows[i].History))
		for j, history := range st.Session.Windows[i].History {
			out.Windows[i].History[j] = append([]string(nil), history...)
		}
	}
	return &out
}

// setSession records what the next launch restores. An empty session — no
// window worth remembering — clears the record, so a plain quit with every
// window closed leaves nothing behind.
func (st *persistedState) setSession(s *sessionState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s == nil || len(s.Windows) == 0 {
		if st.Session == nil {
			return
		}
		st.Session = nil
		st.saveLocked()
		return
	}
	// A live session replaces whatever 清除会话记录 left behind.
	st.sessionCleared = false
	st.Session = s
	st.saveLocked()
}

// clearSession forgets the saved session and keeps it forgotten until the
// next launch: the quit's capture leaves the record alone while the flag
// stands, so 清除会话记录 means nothing comes back when the app closes.
func (st *persistedState) clearSession() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Session = nil
	st.sessionCleared = true
	st.saveLocked()
}

// sessionClearedNow reports whether 清除会话记录 was used since this run
// started.
func (st *persistedState) sessionClearedNow() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessionCleared
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
		st.path = filepath.Join(dir, stateDirName, "state.json")
		for _, name := range []string{stateDirName, legacyStateDirName} {
			data, err := os.ReadFile(filepath.Join(dir, name, "state.json"))
			if err != nil {
				continue
			}
			if err := json.Unmarshal(data, st); err != nil {
				// A corrupt file is replaced, but the store still owns the
				// path: the next change overwrites it.
				st = &persistedState{path: st.path}
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
// directory is what completes the move away from the pre-rename one. A store
// with no path is one built in memory, and writes nothing.
func (st *persistedState) saveLocked() {
	if st.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(st.path, data, 0o644)
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
	st.noteFreqLocked(path, 3) // a pin is a stated favorite
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
	st.noteFreqLocked(path, 1) // opening ranks in the usage ledger
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

// pushVisit records that the user opened folder dir, newest first,
// deduplicated and capped at maxVisited. Special pages (::{home} and the
// rest) are not folders and are skipped. As pushRecent, a change already at
// the head only bumps its time, without a rewrite.
func (st *persistedState) pushVisit(dir string) {
	if dir == "" || strings.HasPrefix(dir, "::{") {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now().UnixMilli()
	st.Visited = slices.DeleteFunc(st.Visited, func(v visitRecord) bool {
		return v.Path == dir
	})
	st.Visited = slices.Insert(st.Visited, 0, visitRecord{Path: dir, At: now})
	if len(st.Visited) > maxVisited {
		st.Visited = st.Visited[:maxVisited]
	}
	st.noteFreqLocked(dir, 1) // visiting ranks in the usage ledger
	st.saveLocked()
}

// visitList returns the visited folders, newest first, up to n when n > 0.
func (st *persistedState) visitList(n int) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	end := len(st.Visited)
	if n > 0 && n < end {
		end = n
	}
	out := make([]string, 0, end)
	for _, v := range st.Visited[:end] {
		out = append(out, v.Path)
	}
	return out
}

// forgetVisit drops a folder from 最近位置, for one that no longer exists.
func (st *persistedState) forgetVisit(dir string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	kept := slices.DeleteFunc(st.Visited, func(v visitRecord) bool { return v.Path == dir })
	if len(kept) == len(st.Visited) {
		return
	}
	st.Visited = kept
	st.saveLocked()
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

// remoteStorages returns a copy of the remote server's mounts
// (remote_storage.go).
func (st *persistedState) remoteStorages() []remoteStorage {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]remoteStorage(nil), st.RemoteStorages...)
}

// setRemoteStorages replaces the mounts.
func (st *persistedState) setRemoteStorages(list []remoteStorage) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.RemoteStorages = append([]remoteStorage(nil), list...)
	st.saveLocked()
}

// remoteShowHidden reports the remote pages' hidden-file switch.
func (st *persistedState) remoteShowHidden() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.RemoteShowHidden
}

// setRemoteShowHidden persists the remote pages' hidden-file switch.
func (st *persistedState) setRemoteShowHidden(on bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.RemoteShowHidden = on
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

// pluginSetting returns the plugin's stored value for the key, empty when
// the plugin never set it. A nil map (a state file from before the field
// existed) reads as empty.
func (st *persistedState) pluginSetting(id, key string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.PluginSettings[id][key]
}

// setPluginSetting records the plugin's value for the key, lazily
// allocating the maps an old state file left nil, and saves. Setting the
// same value again skips the write, as setPlugin does.
func (st *persistedState) setPluginSetting(id, key, val string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.PluginSettings == nil {
		st.PluginSettings = map[string]map[string]string{}
	}
	if st.PluginSettings[id] == nil {
		st.PluginSettings[id] = map[string]string{}
	}
	if st.PluginSettings[id][key] == val {
		return
	}
	st.PluginSettings[id][key] = val
	st.saveLocked()
}

// shareAll returns a copy of the saved shares, newest first (remote_share.go).
func (st *persistedState) shareAll() []remoteShare {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]remoteShare(nil), st.Shares...)
}

// openWithDefault returns the application 始终使用 recorded for the
// extension, empty when none is. Extensions are stored lower-case; a nil
// map (a state file from before the field existed) reads as none.
func (st *persistedState) openWithDefault(ext string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.OpenWith[strings.ToLower(ext)]
}

// setOpenWithDefault records app as the extension's 打开方式 default, or
// clears the record when app is empty — 去掉始终使用. Writing the same
// value again skips the save, as setPlugin does.
func (st *persistedState) setOpenWithDefault(ext, app string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	ext = strings.ToLower(ext)
	if st.OpenWith == nil {
		if app == "" {
			return
		}
		st.OpenWith = map[string]string{}
	}
	if app == "" {
		if _, ok := st.OpenWith[ext]; !ok {
			return
		}
		delete(st.OpenWith, ext)
		if len(st.OpenWith) == 0 {
			st.OpenWith = nil
		}
	} else if st.OpenWith[ext] == app {
		return
	} else {
		st.OpenWith[ext] = app
	}
	st.saveLocked()
}

// openWithDefaults returns a copy of the recorded extension→app map, for
// the settings dialog's 打开方式 section.
func (st *persistedState) openWithDefaults() map[string]string {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]string, len(st.OpenWith))
	for ext, app := range st.OpenWith {
		out[ext] = app
	}
	return out
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
