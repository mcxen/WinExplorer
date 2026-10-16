package main

import (
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
)

// tabState holds the navigation and listing of one Explorer tab. It is the
// state a.path, a.entries, a.selected, ... used to belong to the app; moving
// them here lets several tabs share the window.
type tabState struct {
	path    string
	entries []Entry
	loadErr string

	history  []string
	histIdx  int
	selected map[string]bool
	anchor   string

	query string

	renaming    string
	renameBuf   string
	editingPath bool
	pathDraft   string

	// search is this tab's query walk state (search.go).
	search searchState
}

// App holds the window chrome state and the tab collection. The active tab's
// fields are reached through cur() so the rest of the code reads a.path as
// before.
type App struct {
	tabs      []*tabState
	activeTab int

	sortKey        SortKey
	sortAsc        bool
	mode           ViewMode
	showHidden     bool
	showExt        bool    // 查看>显示>文件扩展名 off hides known extensions
	compact        bool    // 查看>紧凑视图: tighter rows
	itemCheckboxes bool    // 查看>显示>项目复选框
	grouping       SortKey // 分组依据; groupNone when off

	clipPaths []string
	clipCut   bool

	// sysClip caches the file paths on the system clipboard, refreshed at
	// most every 400ms so the paste affordances can check it per frame
	// without a native read every frame.
	sysClip   []string
	sysClipAt time.Time

	navWidth    float32
	previewOpen bool    // Windows preview pane toggle
	previewW    float32 // pane width, 220–460

	// statusMsg is shown by the status bar until the next navigation.
	statusMsg string

	// win is the main window; the title bar's caption buttons call it.
	win *mygo.Window
	// homePath is the user's home directory, the 主文件夹 nav place.
	homePath string
	// navPCExpanded is the 此电脑 tree node's expanded state.
	navPCExpanded bool

	// focusSearch is set by Ctrl+E/Ctrl+F and consumed by the search
	// field, which takes the focus on the next frame.
	focusSearch bool
	// quickLook is the 空格 quick preview modal (quicklook.go).
	quickLook bool
	// propsOpen is the 属性 dialog modal (propsdialog.go).
	propsOpen bool
	// shellExts are the registered context-menu commands (extensions.go).
	shellExts []shellExt
	// store is the persisted 快速访问 pins, 最近使用的文件 and 回收站
	// restore log (statestore.go).
	store *persistedState
	// undo is the Ctrl+Z stack of file operations (undo.go).
	undo []undoOp

	// preview holds the loaded preview for the selected file; replaced
	// atomically once a background load finishes (see preview.go).
	preview atomic.Pointer[previewState]
	// previewGen increments per requestPreview so a slow load cannot
	// replace a newer selection's result.
	previewGen uint64
}

type ViewMode int

const (
	ViewDetails ViewMode = iota
	ViewList
	ViewSmallIcons
	ViewMediumIcons
	ViewLargeIcons
	ViewExtraLarge
)

type SortKey int

const (
	SortName SortKey = iota
	SortDate
	SortType
	SortSize
)

// groupNone turns 分组依据 off.
const groupNone SortKey = -1

type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Ext     string // lower, no dot; empty for dirs

	// Dir is the parent folder's path, shown in the search result's 位置
	// column; empty for regular listings.
	Dir string
	// VolFree/VolTotal carry a drive's capacity for the 此电脑 page's
	// usage bars; zero otherwise.
	VolFree, VolTotal int64
}

type Crumb struct{ Name, Path string }
type Place struct{ Name, Path, Kind string }

const (
	pathThisPC  = "::{this-pc}"
	pathHome    = "::{home}"
	pathGallery = "::{gallery}"
	pathTrash   = "::{trash}"
)

func newTab(path string) *tabState {
	return &tabState{
		path:     path,
		selected: map[string]bool{},
		history:  []string{path},
		histIdx:  0,
	}
}

// cur returns the active tab.
func (a *App) cur() *tabState { return a.tabs[a.activeTab] }

// allSelected gathers the selection across every tab, so a copy or cut made
// in one tab can be pasted after switching.
func (a *App) allSelected() []string {
	var out []string
	for _, t := range a.tabs {
		for p := range t.selected {
			out = append(out, p)
		}
	}
	return out
}
