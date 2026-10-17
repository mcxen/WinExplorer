package main

import (
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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
	// anchor is where Shift ranges start, as Explorer's; cursor is the item
	// the keyboard moves from, which a click, Shift+click or an arrow key
	// moves as well.
	anchor string
	cursor string

	query string

	renaming    string
	renameBuf   string
	editingPath bool
	pathDraft   string

	// The pane's views keep their own states so the keyboard can raise the
	// row it moved to into view: a List for each row-based view, a Grid for
	// the icon views. gridCols and gridRows are the icon grid's shape, as
	// measured in the frame before, which sizes the arrows' row steps.
	details  ui.ListState
	list     ui.ListState
	grid     ui.GridState
	gridCols int
	gridRows int
	// reveal is the path the keyboard moved to: the view showing it scrolls
	// it into view in that frame (handleKeys clears it each frame).
	reveal string

	// marquee is the rubber-band selection's state while the pointer drags
	// on the pane's empty space, and itemBounds the window box of every row
	// or tile the frame before laid out, which the band hit-tests against.
	marquee    marqueeState
	itemBounds map[string]ui.Rect

	// search is this tab's query walk state (search.go).
	search searchState
	// hlCache is the parsed query the pane highlights with (search.go):
	// parsing on every row of every frame would show.
	hlCache struct {
		query  string
		parsed searchQuery
	}
}

// marqueeState tracks a rubber-band drag: where it began and where the
// pointer is now, both in window DIPs, the modifiers held as it began, and
// the selection as it was before the drag, which Shift and Ctrl build on.
type marqueeState struct {
	on     bool
	x0, y0 float32
	x1, y1 float32
	mods   ui.Modifiers
	base   map[string]bool
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
	iconZoom       float32 // 查看>放大/缩小: icon views' size, 1 being the mode's own
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
	// fieldFocused is whether the search field held the keyboard focus in
	// the frame before: a text field keeps the arrows to itself, as ↑/↓
	// and the Page keys are not its own and would otherwise move the pane's
	// selection under what the user is typing. The rename and path fields
	// stop handleKeys outright.
	fieldFocused bool
	// quickLook is the 空格 quick preview modal (quicklook.go).
	quickLook bool
	// propsOpen is the 属性 dialog modal (propsdialog.go).
	propsOpen bool
	// batch is the 批量重命名 dialog's state (batchrename.go).
	batch batchRenameState
	// settingsOpen is the 设置 dialog modal.
	settingsOpen bool
	// bootLogged reports the first-frame timing log has fired.
	bootLogged bool
	// shellExts are the registered context-menu commands (extensions.go);
	// shellExtErrs are the entries the registry could not keep, shown in
	// the settings dialog's 扩展 section until the user fixes the file.
	shellExts    []shellExt
	shellExtErrs []shellExtLoadError
	// extEdit is the 扩展 editor dialog's state (settingsdialog.go): -1
	// adds a new extension, otherwise it edits shellExts' index.
	extEdit extEditState
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
	pathDisks   = "::{disks}"
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

// loadPersistedSettings copies the persisted settings into the app's
// fields, so a new window starts the way the settings page left them.
func (a *App) loadPersistedSettings() {
	s := a.store.settings()
	a.showHidden = s.ShowHidden
	a.showExt = s.ShowExt
	a.compact = s.Compact
	a.itemCheckboxes = s.ItemCheckboxes
	a.previewOpen = s.PreviewOpen
	a.mode = ViewMode(s.Mode)
}
