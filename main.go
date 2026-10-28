package main

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/diskhealth"
	"sunvale/internal/applog"
)

// bootStart is when the process started; the first-frame record in view()
// measures perceived startup against it.
var bootStart = time.Now()

// apps is every open window's App, so closing a window can stop its
// preview's audio and drop its state.
var (
	appsMu sync.Mutex
	apps   []*App
)

func registerApp(a *App) {
	appsMu.Lock()
	defer appsMu.Unlock()
	apps = append(apps, a)
}

// unregisterApp retires a closed window's app: audio stops, tickers end,
// and the tab state becomes garbage.
func unregisterApp(a *App) {
	appsMu.Lock()
	for i, x := range apps {
		if x == a {
			apps = append(apps[:i], apps[i+1:]...)
			break
		}
	}
	appsMu.Unlock()
	if st := a.preview.Load(); st != nil && st.media.player != nil {
		st.media.player.stop()
	}
	a.quickLook = false
}

func main() {
	// 日志最先启动（applog_glue.go）：Windows GUI 程序没有控制台，
	// 之后的启动错误和顶层 panic 都要落到日志文件里。
	// --version / --help 只打印后退出，不起界面、不抢单实例锁。
	if cliFlags(os.Args[1:], os.Stdout) {
		return
	}
	startLogging()
	startPprof()
	defer applog.Close()
	defer applog.Recover("main", true)
	// The updater plugin's window is native UI, as this app's windows
	// are: it needs no webview.
	mygo.Use(native.Plugin)
	// One process per user (singleinstance.go): a second launch raises the
	// running window and exits before building any of its own.
	if !installSingleInstance() {
		return
	}
	store := loadState()
	applyLogLevel(store.settings())
	// 自动更新按镜像列表换线下载（updatemirror.go），签名校验仍由 mygo 完成。
	installUpdateMirrors(store)
	// The 外观 mode the settings dialog last saved, before any window
	// builds: a forced source applies at Run, so it decides the tokens
	// WhenReady resolves for the first frame.
	mygo.Theme.SetSource(appearanceSource(store.settings().Appearance))
	// A system appearance change re-resolves the tokens and redraws: mygo
	// repaints its own widgets, and this is what keeps the app's painted
	// surfaces in step with them.
	mygo.Theme.OnUpdated(repaintAppearance)
	// 恢复上次会话 (session.go): the windows the last run had, each with its
	// tabs and its box, unless the setting is off. The first one is built
	// here and shown in WhenReady; the rest follow it once the first window
	// exists, so a second-start failure cannot leave no window at all.
	session := store.session()
	restore := session != nil && store.settings().restoreSession()
	var app *App
	var rest []windowSession
	if restore {
		app = newSessionApp(store, session.Windows[0], 0)
		rest = session.Windows[1:]
	} else {
		app = newAppWithStore(store)
	}
	registerApp(app)
	// 命令行上的目录/文件（cli.go）：冷启动时在第一个窗口打开。
	if wd, err := os.Getwd(); err == nil {
		app.openCLITargets(cliTargets(os.Args[1:], wd), true)
	}
	// The archive associations in mygo.json open a folder for the
	// archive: a new window shows it, ready to 全部解压.
	mygo.App.OnOpenFile(func(path string) {
		if _, done := cliHandled.LoadAndDelete(path); done {
			return // 命令行已在标签页里打开并选中
		}
		app.openArchiveWindow(path)
	})
	mygo.App.WhenReady(func() {
		// The appearance is known now, before any window builds: re-resolve
		// the design tokens so the windows show the system theme.
		resolveTokens()
		logStartupEnv()
		win := newAppWindow(app)
		win.OnClosed(func() {
			app.saveWindowSession()
			unregisterApp(app)
		})
		// The rest of the restored windows, made after the first is up. The
		// focused one comes to the front; the others stay behind it.
		for i, ws := range rest {
			other := newSessionApp(store, ws, i+1)
			registerApp(other)
			ow := newAppWindow(other)
			ow.OnClosed(func() {
				other.saveWindowSession()
				unregisterApp(other)
			})
			if session.Active == i+1 {
				ow.Focus()
			}
		}
		// The search index (searchindex.go) warms in the background: the
		// Ctrl+K palette answers from the last walk's cache at once, while the
		// volumes are walked again behind it.
		go startSearchIndex()
		// 右键菜单要知道有没有终端：$PATH 查找放后台（viewguard.go）。
		go warmTerminal()
		// Timed jobs (schedule.go): minute ticker inside the process,
		// no OS cron. Must start after the store exists.
		startScheduleRunner()
		// The remote access plugin gates the server: the account may have
		// it enabled, but a switched-off plugin keeps the LAN closed for
		// the run — the settings dialog's switch brings it back.
		if app.remoteConf().Enabled && app.pluginEnabled(remotePluginID) {
			go app.remoteStart()
		}
		// The tray (tray.go) is the minimize-to-tray rest stop and the
		// quit the taskbar no longer shows; a desktop without a tray
		// area keeps every other behavior, so the failure is only logged.
		if _, err := setupTray(); err != nil {
			slog.Warn("tray init failed", "err", err)
		}
		// Ctrl+K summons the search panel from any app, not only from a
		// focused file window. A bind that fails leaves the window shortcut.
		registerSearchHotkey(app.store.settings().SearchHotkey)
	})
	// remoteStop waits for the requests in flight, so it runs once the
	// windows are gone and the event loop is about to stop, not per window.
	// The size engine's cache and the search index land on disk first: they
	// are local and fast, while remoteStop waits on the network.
	// A quit (the tray's 退出晴谷, Cmd+Q) is the only close that ends the
	// process. It is marked before the windows are asked to close, so their
	// OnClose lets that one through instead of hiding them again.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { appQuitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		// 恢复上次会话: the tabs and boxes of the windows still open, before
		// anything closes. A window's own close already saved its record, so
		// this is the final word for the running ones.
		captureSession(store)
		flushDirStats()
		flushSearchIndex()
		app.remoteStop()
		closeAssistantMCP()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// openArchiveWindow opens a window showing the folder an archive lives
// in, with the archive selected, as Explorer does for an opened file
// type it owns.
func (a *App) openArchiveWindow(path string) {
	dir := filepath.Dir(path)
	a.openNewWindow(dir)
	a.selectOnly(path)
}

// newAppWindow opens the window an App drives. Ctrl+N opens another one
// over its own App, so each window has its own tabs, undo and clipboard.
func newAppWindow(app *App) *mygo.Window {
	opts := mygo.WindowOptions{
		Title:     "晴谷",
		Width:     1100,
		Height:    720,
		MinWidth:  640,
		MinHeight: 420,
		Frameless: true,
		// The Windows 11 Mica backdrop for a window with tabs in its
		// title bar (an under-window material elsewhere): the chrome
		// bands draw no background over it, and paint their tokens only
		// where no material shows (see glass.go).
		Vibrancy: mygo.VibrancyTabbed,
		Content:  ui.View(app.view),
	}
	// A restored window opens where it was closed; a fresh one is centered at
	// the default size (session.go).
	if g := app.geom; g != nil {
		opts.X, opts.Y, opts.Width, opts.Height = g.x, g.y, g.w, g.h
	}
	win := mygo.NewWindow(opts)
	app.win = win
	archiveTasks.Lock()
	archiveTasks.onChange = func() { app.postAssist(func() {}) }
	archiveTasks.Unlock()
	taskManager()
	// Minimizing parks the window in the tray (tray.go) instead of the
	// taskbar, so the process — and its background size statistics — keeps
	// running out of sight.
	win.OnMinimize(func() { minimizeToTray(win) })
	// 回到窗口时刷新一次系统剪贴板（别的程序可能刚复制了文件），
	// 视图只读这份缓存，不再逐帧读剪贴板（ops.go）。
	win.OnFocus(app.refreshSystemClipboardAsync)
	// The caption close and the system's close both hide the window in the
	// tray. The process keeps running; 退出晴谷 is what actually quits.
	win.OnClose(func(e *mygo.CloseEvent) {
		if hideWindowToTray(win) {
			e.PreventDefault()
		}
	})
	return win
}

// openNewWindow is Ctrl+N: a new window on the same place, as Explorer's.
func (a *App) openNewWindow(path string) {
	app := newAppWithStore(a.store)
	if !specialPath(path) {
		app.navigate(path)
	}
	registerApp(app)
	win := newAppWindow(app)
	win.OnClosed(func() {
		app.saveWindowSession()
		unregisterApp(app)
	})
}

func newApp() *App { return newAppWithStore(loadState()) }

// newAppWithStore builds one window's worth of app state over the shared
// persisted store: pins, recents and the trash log stay one collection
// across every open window.
func newAppWithStore(store *persistedState) *App {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/"
	}
	a := &App{
		// Explorer opens on the 主文件夹 (Home) page.
		tabs:       []*tabState{newTab(pathHome)},
		activeTab:  0,
		sortKey:    SortName,
		sortAsc:    true,
		mode:       ViewDetails,
		iconZoom:   1,
		showHidden: false,
		showExt:    true,
		grouping:   groupNone,
		navWidth:   248,
		// The 此电脑 tree shows from the start: it is what follows the tab
		// around, and a window whose tree is collapsed shows no sync at all.
		navPCExpanded: true,
		// The 快速访问 and 最近位置 sections start expanded, as the pinned
		// places are the pane's quickest way somewhere.
		navQuickExpanded:  true,
		navRecentExpanded: true,
		homePath:          home,
		previewOpen:       false,
		previewW:          320,
		store:             store,
		// A window opened fresh has no saved record yet (session.go).
		sessionSlot: -1,
	}
	// The assistant's reads score in the process's usage ledger. One hook
	// for every window: they share the store.
	assistNoteUsed = func(paths ...string) {
		for _, p := range paths {
			store.notePath(p, 1)
		}
	}
	// shellExts are the registered context-menu commands (extensions.go);
	// the load's per-entry problems stay on the app, for the settings
	// dialog's 扩展 section, instead of only flashing the status bar.
	exts, extErrs, extErr := loadShellExtensions()
	a.shellExts = exts
	a.shellExtErrs = extErrs
	if extErr != nil {
		a.statusMsg = "扩展配置无效：" + extErr.Error()
	}
	// The view settings the settings dialog last saved apply from the
	// first listing on, so they land before the first reload.
	a.loadPersistedSettings()
	// The open-with and gallery registries are process-wide, and both scan
	// lazily: the registry starts with the first 打开方式 menu, the
	// gallery with the first visit of its page, so launching costs neither.
	a.reload()
	return a
}

// view stacks the Explorer chrome and a body row that takes the rest of
// the window. Each band fills the width; the body and the file pane take
// the remaining height.
func (a *App) view(c *ui.Context) {
	enterView()
	defer leaveView()
	if !a.bootLogged {
		a.bootLogged = true
		// A background disk probe landing redraws the window, so the
		// 磁盘健康 page does not stay on "正在读取…" until something else
		// happens to repaint. The callback itself is the dispatcher
		// (tray.go's diskUpdateNotify), process-wide because every window
		// shows the same disks — and the tray's menu rebuilds from the same
		// landings.
		diskhealth.SetOnUpdate(diskUpdateNotify)
		diskUpdateOn(repaint)
		// One Info record per launch: the diagnostic log is the right gate
		// for a startup metric, not an environment variable.
		slog.Info("first frame", "since_boot_ms", time.Since(bootStart).Milliseconds())
	}
	a.handleKeys(c)
	a.updateSearch()
	a.searchBoxFrame()
	// A fresh statusMsg is an operation's outcome: show it as a toast as well,
	// with a 撤销 button when the undo stack grew for it. MyGo's own toast face
	// builds its viewport only while a toast shows — Onevra's components.Toasts
	// keeps an empty one over the window that takes the clicks a dialog's
	// backdrop dismissal needs, so it is not hosted here.
	if a.statusMsg != "" && a.statusMsg != a.statusToasted {
		a.statusToasted = a.statusMsg
		if len(a.extractSources) > 0 {
			// A finished unpack offers its next step on the toast: keep only
			// what the archives became.
			srcs := a.extractSources
			a.extractSources = nil
			c.ToastAction(a.statusMsg, "删除原归档", func() {
				a.trashPaths(srcs)
			})
		} else if a.undoSeq != a.undoSeqToasted {
			a.undoSeqToasted = a.undoSeq
			c.ToastAction(a.statusMsg, "撤销", a.doUndo)
		} else {
			c.Toast(a.statusMsg)
		}
	}
	// The frame theme carries the app's Windows 11 tokens to MyGo's widgets
	// and the Onevra components, before anything under it builds.
	a.syncVisual()
	c.SetTheme(appTheme(c))
	// The root paints the chrome color only where the window shows no
	// material; with one, the bands that leave themselves clear let the
	// Mica through, and the file pane's opaque content surface covers the
	// rest.
	root := windowBG(c, ui.Column(c).Fill())
	if visualV2 && !c.Vibrancy() {
		root.LinearGradient(v2Wash())
	}
	root.Children(func() {
		a.tabStrip(c)
		a.commandBar(c)
		a.navRow(c)
		a.bodyRow(c)
		a.statusBar(c)
		// Esc in the assistant composer Focuses this sink so the file list
		// owns the keyboard again (handleKeys). Size 0 keeps it invisible.
		sink := ui.Box(c.Key("file-pane-focus")).Size(0, 0).Focusable().Label("文件列表")
		if a.assist.blurToPane {
			sink.Focus()
			a.assist.blurToPane = false
			a.assist.composerFocused = false
		}
	})
	a.quickLookModal(c)
	a.propertiesModal(c)
	a.shareModal(c)
	a.settingsModal(c)
	a.extEditModal(c)
	a.batchRenameModal(c)
	a.searchBoxModal(c)
}

// bodyRow is the chrome's body: the navigation pane, then the file pane and
// the preview pane beside it. components.Split owns the boundary between the
// two — its one-DIP line, the drag handle over it, the keyboard resize (Tab to
// the divider, the arrow keys to move it, Home and End for the ends) and the
// splitter role — while the app keeps the pane's width, so it survives the
// frame and stays where clampNavWidth holds it.
func (a *App) bodyRow(c *ui.Context) {
	// Split bounds the first pane by minima, not by a ceiling: the content's
	// minimum is raised to what leaves the pane its own 420 DIP limit
	// (navMaxWidth). The split spans the window, so its width is the window's,
	// and the one DIP its divider takes comes off the room the panes share.
	w, _ := c.Size()
	secondMin := max(float32(navContentMin), w-1-navMaxWidth)
	split := components.Split(c, &a.navWidth,
		func() { a.sidebar(c) },
		func() { a.contentPanes(c) },
		components.SplitOptions{FirstMin: navMinWidth, SecondMin: secondMin})
	a.navWidth = clampNavWidth(a.navWidth)
	split.Grow(1).FillWidth()
}

// contentPanes is the room right of the navigation pane: the file pane, with
// the preview pane beside it while Alt+P has it open, and the assistant when
// it is open. Full screen gives that pane the whole room; the sidebar stays,
// so the folder list is one click away when the chat is closed or restored.
func (a *App) contentPanes(c *ui.Context) {
	if a.assistantOpen && a.assist.full {
		ui.Row(c).FillWidth().FillHeight().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.assistantPane(c)
		})
		return
	}
	row := ui.Row(c).FillWidth().FillHeight().MinHeight(0).AlignItems(ui.Stretch)
	if visualV2 {
		// v2: keep classic's FillHeight Row (cross-axis % height in the
		// Split Column), gutter via Margin, card chrome on that same row.
		row.Margin(0, v2Gutter, v2Gutter, 2)
		a.contentRow(c, v2Card(row))
		return
	}
	a.contentRow(c, classicContentFrame(row))
}

func (a *App) contentRow(c *ui.Context, row ui.Element) {
	row.Children(func() {
		a.flipPane(c)
		if a.previewOpen {
			a.previewPane(c)
		}
		if a.assistantOpen {
			a.assistantPane(c)
		}
	})
}

// handleKeys maps Explorer shortcuts. A focused text field consumes its
// own editing keys before Context.Shortcut sees them, so Escape and Enter
// stay with the rename field and the path editor.
func (a *App) handleKeys(c *ui.Context) {
	// reveal is not cleared here. A keyboard move sets it before the pane
	// builds, and the pane scrolls to it and clears it. A navigation that
	// happens while the pane is already built — the search box, which is
	// drawn after the list — leaves the request for the next frame, which
	// would never see it if this ran first and dropped it.
	if a.cur().renaming != "" {
		// The inline rename's editor (components.Text) owns only its caret:
		// Escape goes to the shortcuts, and cancels the edit.
		if c.Shortcut(0, ui.KeyEscape) {
			a.cancelRename()
		}
		return
	}
	// The tab keys work from the address bar's edit too, as Explorer's:
	// bringing another tab forward drops the edit.
	if a.handleTabKeys(c) {
		return
	}
	if a.cur().editingPath {
		return
	}
	// The command-bar flyout owns ↑↓←→ Enter Esc (menuflyout.go); leave the
	// pane and the nav tree alone while it is open.
	if a.fly.open != "" {
		return
	}
	// The assistant composer holds the keyboard: Enter sends there, Esc
	// returns focus to the file list (wireComposerFocus). File-pane binds
	// must not fire over a focused draft.
	if a.assist.composerFocused {
		return
	}
	// The navigation pane owns its cursor after a row click. Its arrows move
	// that cursor; Enter/Space is the explicit navigation action.
	if a.navTreeKeys(c) {
		return
	}
	// The quick preview's Space-close and ←/→ keys live inside the modal
	// (quicklook.go): Context.Shortcut is inert behind a dialog, so
	// bindings here never fire while it is open. Outside it ←/→ belong to
	// the pane's keyboard navigation.
	switch {
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyJ), c.Shortcut(ui.Cmd|ui.Shift, ui.KeyJ):
		a.toggleTasksView()
	case a.tasksFlipped && c.Shortcut(0, ui.KeyEscape):
		a.tasksFlipped = false
	case c.Shortcut(0, ui.KeyBackspace):
		a.up()
	case c.Shortcut(ui.Alt, ui.KeyLeft), c.Shortcut(ui.Cmd, ui.KeyBracketLeft):
		a.back()
	case c.Shortcut(ui.Alt, ui.KeyRight), c.Shortcut(ui.Cmd, ui.KeyBracketRight):
		a.forward()
	case c.Shortcut(ui.Alt, ui.KeyUp):
		a.up()
	case c.Shortcut(0, ui.KeyEnter):
		if len(a.selectedEntries()) > 0 {
			a.doOpen()
		} else if !a.assistantOpen {
			// A bare Enter with nothing to open goes to the assistant, and
			// the pane comes up on its history column: the global 回车 the
			// window listens for, next to the toolbar's 助手.
			a.toggleAssistant()
			a.assist.showHist = true
		}
	case c.Shortcut(0, ui.KeyDelete), c.Shortcut(ui.Cmd, ui.KeyBackspace), c.Shortcut(ui.Ctrl, ui.KeyD):
		a.doDelete()
	case c.Shortcut(0, ui.KeyF2):
		a.doRenameStart()
	case c.Shortcut(0, ui.KeySpace):
		if a.quickLook {
			a.quickLook = false
		} else {
			a.openQuickLook()
		}
	case c.Shortcut(0, ui.KeyF5):
		a.doRefresh()
	case c.Shortcut(ui.Cmd, ui.KeyC), c.Shortcut(ui.Ctrl, ui.KeyC):
		a.doCopy()
	case c.Shortcut(ui.Cmd, ui.KeyZ), c.Shortcut(ui.Ctrl, ui.KeyZ):
		a.doUndo()
	case c.Shortcut(ui.Shift, ui.KeyDelete):
		a.doPermanentDelete()
	case c.Shortcut(ui.Alt, ui.KeyEnter):
		if sel := a.selectedEntries(); len(sel) == 1 {
			a.showProperties()
		}
	case c.Shortcut(ui.Ctrl, ui.KeyComma), c.Shortcut(ui.Cmd, ui.KeyComma):
		a.settingsOpen = true
	case c.Shortcut(ui.Cmd, ui.KeyE), c.Shortcut(ui.Ctrl, ui.KeyE),
		c.Shortcut(ui.Ctrl, ui.KeyF), c.Shortcut(ui.Cmd, ui.KeyF), c.Shortcut(0, ui.KeyF3):
		a.focusSearch = true
	case a.searchShortcutDown(c):
		// The global bind already toggles the panel, and it is delivered
		// even while this window is focused. Handling it here as well would
		// open and close on the one key. With no global bind — a headless
		// test, or a desktop that refused the shortcut — the window takes it.
		if !searchHotkey {
			a.openSearchBox()
		}
	case c.Shortcut(0, ui.KeyEscape):
		// The inline rename's editor (components.Text) owns only its caret and
		// lets Escape through to the shortcuts, so the window cancels the edit
		// here. A modal or a focused field takes its Escape first.
		if a.cur().renaming != "" {
			a.cancelRename()
		}
	case c.Shortcut(ui.Alt, ui.KeyD), c.Shortcut(ui.Ctrl, ui.KeyL), c.Shortcut(0, ui.KeyF4):
		a.beginPathEdit()
	case c.Shortcut(0, ui.KeyF11):
		if a.win != nil {
			a.win.ToggleMaximize()
		}
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyC), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyC):
		a.doCopyAsPath()
	case c.Shortcut(ui.Cmd, ui.KeyX), c.Shortcut(ui.Ctrl, ui.KeyX):
		a.doCut()
	case c.Shortcut(ui.Cmd, ui.KeyV), c.Shortcut(ui.Ctrl, ui.KeyV):
		a.doPaste()
	case c.Shortcut(ui.Cmd, ui.KeyA), c.Shortcut(ui.Ctrl, ui.KeyA):
		a.selectAll()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyEqual):
		a.zoomIn()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyMinus), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyMinus):
		a.zoomOut()
	case c.Shortcut(ui.Alt, ui.KeyP):
		a.togglePreview()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyN):
		a.doNewFolder()
	}
	// Ctrl+Shift+1..6: view styles, as Explorer's.
	styles := []struct {
		key  ui.Key
		mode ViewMode
	}{
		{ui.Key1, ViewExtraLarge},
		{ui.Key2, ViewLargeIcons},
		{ui.Key3, ViewMediumIcons},
		{ui.Key4, ViewSmallIcons},
		{ui.Key5, ViewList},
		{ui.Key6, ViewDetails},
	}
	for _, st := range styles {
		if c.Shortcut(ui.Ctrl|ui.Shift, st.key) || c.Shortcut(ui.Cmd|ui.Shift, st.key) {
			a.setMode(st.mode)
			return
		}
	}
	// The pane's keyboard navigation: the arrows step the selection, Home
	// and End go to its ends, Page Up and Page Down by a screenful, and
	// Shift extends from the anchor as Shift+click does. A modal owns the
	// keys while it is up, and the search field keeps them while it has
	// the focus.
	if a.quickLook || a.propsOpen || a.fieldFocused || a.assist.composerFocused {
		return
	}
	switch {
	case c.Shortcut(0, ui.KeyUp):
		a.moveSelection(-1, 0, false)
	case c.Shortcut(0, ui.KeyDown):
		a.moveSelection(1, 0, false)
	case c.Shortcut(0, ui.KeyLeft):
		a.moveSelection(0, -1, false)
	case c.Shortcut(0, ui.KeyRight):
		a.moveSelection(0, 1, false)
	case c.Shortcut(0, ui.KeyHome):
		a.moveSelectionEnd(false, false)
	case c.Shortcut(0, ui.KeyEnd):
		a.moveSelectionEnd(true, false)
	case c.Shortcut(0, ui.KeyPageUp):
		a.moveSelectionPage(-1, false)
	case c.Shortcut(0, ui.KeyPageDown):
		a.moveSelectionPage(1, false)
	case c.Shortcut(ui.Shift, ui.KeyUp):
		a.moveSelection(-1, 0, true)
	case c.Shortcut(ui.Shift, ui.KeyDown):
		a.moveSelection(1, 0, true)
	case c.Shortcut(ui.Shift, ui.KeyLeft):
		a.moveSelection(0, -1, true)
	case c.Shortcut(ui.Shift, ui.KeyRight):
		a.moveSelection(0, 1, true)
	case c.Shortcut(ui.Shift, ui.KeyHome):
		a.moveSelectionEnd(false, true)
	case c.Shortcut(ui.Shift, ui.KeyEnd):
		a.moveSelectionEnd(true, true)
	case c.Shortcut(ui.Shift, ui.KeyPageUp):
		a.moveSelectionPage(-1, true)
	case c.Shortcut(ui.Shift, ui.KeyPageDown):
		a.moveSelectionPage(1, true)
	}
}

func (a *App) selectOnly(path string) {
	t := a.cur()
	t.selected = map[string]bool{path: true}
	t.anchor = path
	t.cursor = path
	a.requestPreview()
}

func (a *App) toggleSelect(path string) {
	t := a.cur()
	if len(t.selected) == 0 {
		t.selected = map[string]bool{}
	}
	if t.selected[path] {
		delete(t.selected, path)
		if t.anchor == path {
			t.anchor = ""
		}
		a.requestPreview()
		return
	}
	t.selected[path] = true
	t.anchor = path
	t.cursor = path
	a.requestPreview()
}

// selectRange selects every visible entry from the anchor through path,
// replacing the previous selection. With no anchor it selects only path.
func (a *App) selectRange(path string) {
	t := a.cur()
	vis := a.visibleEntries()
	if t.anchor == "" {
		a.selectOnly(path)
		return
	}
	from, to := -1, -1
	for i, e := range vis {
		if e.Path == t.anchor {
			from = i
		}
		if e.Path == path {
			to = i
		}
	}
	if from < 0 || to < 0 {
		a.selectOnly(path)
		return
	}
	if from > to {
		from, to = to, from
	}
	next := make(map[string]bool, to-from+1)
	for _, e := range vis[from : to+1] {
		next[e.Path] = true
	}
	t.selected = next
	t.cursor = path
	a.requestPreview()
}

func (a *App) selectAll() {
	t := a.cur()
	vis := a.visibleEntries()
	next := make(map[string]bool, len(vis))
	for _, e := range vis {
		next[e.Path] = true
	}
	t.selected = next
	if t.anchor == "" && len(vis) > 0 {
		t.anchor = vis[0].Path
	}
	a.requestPreview()
}

func (a *App) clearSelection() {
	t := a.cur()
	t.selected = map[string]bool{}
	t.anchor = ""
	t.cursor = ""
	a.requestPreview()
}

// togglePreview flips the preview pane on or off, refreshing its contents
// for the current selection. Closing the pane stops a playing preview,
// as Explorer's preview pane does.
func (a *App) togglePreview() {
	a.previewOpen = !a.previewOpen
	if a.previewOpen {
		a.requestPreview()
		return
	}
	if st := a.preview.Load(); st != nil && st.media.player != nil {
		st.media.player.stop()
	}
}

func (a *App) isSelected(path string) bool {
	return a.cur().selected[path]
}

// selectedEntries returns the selected visible entries, in view order.
func (a *App) selectedEntries() []Entry {
	t := a.cur()
	if len(t.selected) == 0 {
		return nil
	}
	var out []Entry
	for _, e := range a.visibleEntries() {
		if t.selected[e.Path] {
			out = append(out, e)
		}
	}
	return out
}

// invertSelection selects what is visible and unselected, and drops the
// rest, as Explorer's 反向选择.
func (a *App) invertSelection() {
	t := a.cur()
	next := map[string]bool{}
	for _, e := range a.visibleEntries() {
		if !t.selected[e.Path] {
			next[e.Path] = true
		}
	}
	t.selected = next
	t.anchor, t.cursor = "", ""
	a.requestPreview()
}
