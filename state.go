package main

import (
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"sunvale/diskhealth"
	"sunvale/internal/fsentry"
	"sunvale/internal/remote/driver"
	"sunvale/internal/searchquery"
)

// tabState holds the navigation and listing of one Explorer tab. It is the
// state a.path, a.entries, a.selected, ... used to belong to the app; moving
// them here lets several tabs share the window.
type tabState struct {
	// id names the tab for as long as it lives, whatever place it moves to
	// in the strip: the tab's widgets are keyed by it, so a tab dragged or
	// a neighbour closed does not hand its hover, press or editor state to
	// the tab that took its place.
	id      int
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
	// selectPath is the folder a backward move came out of: the listing it
	// lands on selects that row once, then clears it (navigate's
	// selectAfterNav).
	selectPath string

	// marquee is the rubber-band selection's state while the pointer drags
	// on the pane's empty space, and itemBounds the window box of every row
	// or tile the frame before laid out, which the band hit-tests against.
	marquee    marqueeState
	itemBounds map[string]ui.Rect
	// detailsPaneW is the details list's width last frame, which the
	// default columns fit to (files.go fittedCols).
	detailsPaneW float32

	// networkDir says the folder lives on a network mount, measured once
	// per icon view: the tiles read it to decide whether to decode their
	// pictures now or wait until they are looked at. networkWarm is that
	// and the 设置 switch together: the first tiles warm up only when both
	// allow it.
	networkDir  bool
	networkWarm bool

	// search is this tab's query walk state (search.go), and queryScope what
	// its query covers: this folder's subtree, or every mounted volume
	// (Everything's 全部磁盘). The scope is the user's choice, so it outlives
	// the walk it started.
	search     searchState
	queryScope searchScope
	// hlCache is the parsed query the pane marks its hits with (search.go):
	// parsing on every row of every frame would show.
	hlCache struct {
		query  string
		parsed searchquery.Query
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
	fly flyState // the command bar's open flyout (menuflyout.go)

	// tasksFlipped turns the middle area to the task view (tasks_flip.go);
	// tasksTab is its tab and tasksOpen its expanded member lists.
	flip         *flipState // non-nil while the middle area is turning
	flipLowEnd   bool       // frames ran over budget: crossfade, no blur
	flipLanded   time.Time  // when the last flip landed (live-content fade)
	tasksFlipped bool
	tasksTab     int
	tasksOpen    map[string]bool
	// taskAskDraft holds typed passwords for EXTRACT-ASK cards by task id.
	taskAskDraft map[string]string
	// addr is the address bar's edit and completion state (addressbar.go).
	addr      addrState
	tabs      []*tabState
	activeTab int
	// closedTabs is the reopen stack of 重新打开关闭的标签页, newest last.
	closedTabs []closedTab
	// tabReveal asks the strip to scroll the front tab into view in the
	// next frame: set whenever a tab comes forward.
	tabReveal bool
	// tabDrag is the tab being dragged along the strip and how far the
	// pointer went since it last changed places (chrome_tab.go).
	tabDrag struct {
		id  int
		acc float32
	}
	// tabDropHover is the tab a file drag has hovered long enough to consider
	// bringing forward. The id is stable while tabs reorder, and zero means
	// there is no delayed switch pending.
	tabDropHover struct {
		id    int
		since time.Time
	}

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
	// assistantOpen is the right-hand AI pane (assistant.go).
	assistantOpen bool
	assist        assistantUI
	// lastArchivePassword is the candidate that opened the previous
	// encrypted archive. It is memory only and is tried first next time.
	lastArchivePassword string
	// extractSources are the archives the latest doExtract unpacked in
	// full; the toast offers moving them to the bin (archive.go).
	extractSources []string

	// colWidths are the details view's user-adjusted column widths
	// (files.go); zero keeps the column's default.
	colWidths colWidths

	// statusMsg is shown by the status bar until the next navigation.
	statusMsg string
	// statusToasted is the message the window last toasted, so a message
	// showing already is not toasted again while it stands; undoSeq counts
	// undoable operations (pushUndo), and the seq a toast showed decides
	// whether it carries a 撤销 button.
	statusToasted  string
	undoSeq        int
	undoSeqToasted int

	// win is the main window; the title bar's caption buttons call it.
	win *mygo.Window
	// geom is the saved box a restored window opens in (session.go);
	// nil for a window opened fresh, which is centered at the default size.
	geom *windowGeom
	// sessionSlot is the restored session record this window came from, or
	// -1 for a window opened fresh; saveWindowSession updates that record as
	// the window closes (session.go).
	sessionSlot int
	// homePath is the user's home directory, the 主文件夹 nav place.
	homePath string
	// navPCExpanded is the 此电脑 tree node's expanded state.
	navPCExpanded bool
	// navQuickExpanded and navRecentExpanded are the 快速访问 and 最近位置
	// section headings' expanded state: collapsed, the section keeps its
	// heading — a drop on 快速访问 still pins — and hides its rows.
	navQuickExpanded  bool
	navRecentExpanded bool

	// focusSearch is set by Ctrl+E/Ctrl+F and consumed by the search
	// field, which takes the focus on the next frame.
	focusSearch bool
	// searchBox is the Ctrl+K palette's own search state (searchbox.go):
	// one box per window, searching the machine rather than a tab's folder.
	searchBox searchBoxState
	// navTree is the navigation pane's folder tree (navtree.go): which folders
	// are open, the children read for them, and where the current place sits in
	// the pane's list.
	navTree navTree
	// fieldFocused is whether the search field held the keyboard focus in
	// the frame before: a text field keeps the arrows to itself, as ↑/↓
	// and the Page keys are not its own and would otherwise move the pane's
	// selection under what the user is typing. The rename and path fields
	// stop handleKeys outright.
	fieldFocused bool
	// quickLook is the 空格 quick preview modal (quicklook.go).
	quickLook bool
	// quickLookFile, when set, is what the open quick preview shows instead
	// of the selection (an OpenUI MediaList card).
	quickLookFile string
	// imgQV is zoom/pan for the image Space viewer (imageview.go).
	imgQV imgQuickView
	// vidQV is scrub/keyframe state for the video Space viewer (videoview.go).
	vidQV vidQuickView
	// vidFrameGen tags in-flight scrub extracts so a slow ffmpeg cannot
	// overwrite a newer scrub position.
	vidFrameGen uint64
	// docQV is the paged raster state of the document Space viewer (docview.go).
	docQV docQuickView
	// docGen tags in-flight page rasterising so a stale flip is dropped.
	docGen uint64
	// propsOpen is the 属性 dialog modal (propsdialog.go).
	propsOpen bool
	// share is the 分享 dialog's state (sharedialog.go).
	share shareDialog
	// batch is the 批量重命名 dialog's state (batchrename.go).
	batch batchRenameState
	// settingsOpen is the 设置 dialog modal.
	settingsOpen bool
	// hotkeyCapture is the 托盘 page waiting for a new 快速搜索 shortcut.
	hotkeyCapture bool
	// settingsUI is the dialog's category and search box. components.Settings
	// owns the navigation; each window remembers its own.
	settingsUI components.SettingsState
	// settingsAssist holds transient form fields for the 助手 settings page.
	settingsAssist settingsAssistForm
	// settingsSchedule holds transient form fields for the 定时任务 page.
	settingsSchedule settingsScheduleForm
	// settingsScheduleDateStampStr mirrors dateStamp for the shared switch painter.
	settingsScheduleDateStampStr *string
	// bootLogged reports the first-frame timing log has fired.
	bootLogged bool
	// diskWatch stops the 磁盘健康 page's background probe, while that page
	// is the one on screen. The poll keeps the throughput figures moving
	// and is torn down on navigation, so no window probes disks it is not
	// showing.
	diskWatch func()
	// diskRead replaces the disk-health probe in tests, keeping the page's
	// layout assertions independent of the machine running them.
	diskRead func() diskhealth.Report
	// diskCharts and diskVolumeCharts keep the per-disk chart interaction
	// state (which point or slice is selected), one entry per disk id.
	diskCharts       map[string]*chart.State
	diskVolumeCharts map[string]*chart.State
	// shellExts are the registered context-menu commands (extensions.go);
	// shellExtErrs are the entries the registry could not keep, shown in
	// the settings dialog's 扩展 section until the user fixes the file.
	shellExts    []shellExt
	shellExtErrs []shellExtLoadError
	// extCommand lets tests replace sh so they can wait for an extension's
	// completion signal instead of sleeping for its side effect.
	extCommand func(name string, arg ...string) *exec.Cmd
	// extEdit is the 扩展 editor dialog's state (settingsdialog.go): -1
	// adds a new extension, otherwise it edits shellExts' index.
	extEdit extEditState
	// store is the persisted 快速访问 pins, 最近使用的文件 and 回收站
	// restore log (statestore.go).
	store *persistedState
	// driverCache keeps one remote storage driver per storage id, so an
	// SMB session is not re-dialed per request (remote_storage.go).
	driverCache map[uint]driver.Driver
	// undo is the Ctrl+Z stack of file operations (undo.go).
	undo []undoOp
	// dropTookOwnDrag is set when a target of this window took the drag
	// under the pointer (dropops.go), so the native source's completion,
	// which arrives in the same frame, does not reload over what
	// doDropInto reported.
	dropTookOwnDrag bool
	// dragSuggested is the effect the current native drag suggests, from
	// the keys held over it (transfer.Offer.Suggested): a native session
	// takes the input, so this is how a copy drag is told from a move.
	dragSuggested transfer.Operation

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

type Entry = fsentry.Entry

type Crumb struct{ Name, Path string }
type Place struct{ Name, Path, Kind string }

const (
	pathThisPC  = "::{this-pc}"
	pathHome    = "::{home}"
	pathGallery = "::{gallery}"
	pathTrash   = "::{trash}"
	pathDisks   = "::{disks}"
)

// tabSeq hands out tab ids. Tabs are only made on the UI thread.
var tabSeq int

func newTab(path string) *tabState {
	tabSeq++
	return &tabState{
		id:       tabSeq,
		path:     path,
		selected: map[string]bool{},
		history:  []string{path},
		histIdx:  0,
	}
}

// cur returns the active tab.
func (a *App) cur() *tabState { return a.tabs[a.activeTab] }

// colWidths are the details view's user-adjusted column widths; zero
// keeps the column's default (files.go's col*W constants), the name
// column flexing with the pane as before.
type colWidths struct {
	name, date, typ, loc, size float32
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
	a.colWidths = colWidths{
		name: s.ColName, date: s.ColDate, typ: s.ColType,
		loc: s.ColLoc, size: s.ColSize,
	}
}
