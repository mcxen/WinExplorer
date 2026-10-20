package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"sync"

	"github.com/egoist/mygo"
	xdraw "golang.org/x/image/draw"

	"sunvale/diskhealth"
)

// tray.go is the process's one tray entry: the minimize-to-tray rest stop
// and, since a tray menu is where a background app reports from, the
// menu the user opens it for — 快速搜索 (the Ctrl+K palette, searchbox.go),
// the windows, the disks' capacity and live read/write speed (diskhealth's
// probes, refreshed as each one lands), and the switches that decide what
// shows.

//go:embed resources/icon.png
var trayIconRaw []byte

// tray is the process's one tray icon, so a second setupTray hands back the
// one that already exists instead of planting another icon.
var tray *mygo.Tray

// trayNotified reports the minimized-to-tray note has been shown: however
// many windows minimize to the tray, the notification comes once per run.
var trayNotified bool

// appQuitting is set when a real quit starts (the tray's 退出晴谷, or the
// system quit), before the windows are asked to close. A close while it is
// false hides the window in the tray; the flag is what lets the quit close
// them for real.
var appQuitting bool

// trayIconSide is the tray icon's edge in pixels: mygo suggests 16×16 points,
// and 32 covers Retina's 2×.
const trayIconSide = 32

// trayDiskMaxRows is how many disks the menu lists before the rest are
// left to the 磁盘健康 page: a tray menu is a glance, not a report.
const trayDiskMaxRows = 4

// trayDiskStop is the tray's disk probe, running only while the menu shows
// any disk information; nil while it does not.
var trayDiskStop func()

// trayIconPNG shrinks the embedded app icon down to the tray's size. It is a
// function of its own so the headless tests can decode and measure what the
// tray would show.
func trayIconPNG() ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(trayIconRaw))
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, trayIconSide, trayIconSide))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// setupTray plants the tray icon with its menu. It is called in
// App.WhenReady, the point NewTray's after-ready contract means, so calling
// it before then would panic. The menu carries the disk section only while
// a setting asks for it, which is also what starts and stops the probe.
func setupTray() (*mygo.Tray, error) {
	if tray != nil {
		return tray, nil
	}
	icon, err := trayIconPNG()
	if err != nil {
		return nil, err
	}
	// The icon is the app's colored one, not a black menu-bar glyph, so
	// IconIsTemplate stays false.
	t, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    icon,
		ToolTip: "晴谷",
		Menu:    mygo.NewMenu(trayMenuItems(diskhealth.ReadAsync(), traySettings())),
	})
	if err != nil {
		return nil, err
	}
	// A left click restores the window where clicks open the tray menu
	// anyway (macOS); Windows keeps the menu on the right button.
	t.OnClick(func() { trayShowMainWindow() })
	tray = t
	// The dispatcher must be installed before the tray's probe can land:
	// the windows install it on their first frame, which a tray-only run
	// (every window closed) never reaches.
	diskhealth.SetOnUpdate(diskUpdateNotify)
	trayDiskWatch()
	return t, nil
}

// trayMenuItems builds the menu: 快速搜索 and the windows, the disk
// section, the switches over it, and quit. It is a pure function of the
// report and the two settings, so the tests can pin its shape; trayRebuild
// re-runs it as probes land and switches flip.
func trayMenuItems(rep diskhealth.Report, s appSettings) []*mygo.MenuItem {
	items := []*mygo.MenuItem{
		{
			// The palette Ctrl+K opens (searchbox.go), at its 全部磁盘
			// scope: a window is brought up to carry it when none lives.
			Label: "快速搜索…",
			Click: func(*mygo.MenuItem, *mygo.Window) { trayMainApp().openSearchBox() },
		},
		{
			Label: "显示晴谷",
			Click: func(*mygo.MenuItem, *mygo.Window) { trayShowMainWindow() },
		},
		{
			Label: "新建窗口",
			Click: func(*mygo.MenuItem, *mygo.Window) { trayNewWindow() },
		},
		mygo.Separator(),
	}
	shown := trayDiskShown(s)
	capOn, speedOn := shown.capOn, shown.speedOn
	if capOn || speedOn {
		items = append(items, trayDiskItems(rep, capOn, speedOn)...)
		items = append(items, mygo.Separator())
	}
	items = append(items,
		&mygo.MenuItem{
			Type:    mygo.MenuItemCheckbox,
			Label:   "显示磁盘容量",
			Checked: capOn,
			Click:   func(*mygo.MenuItem, *mygo.Window) { traySetDiskShown("cap", !capOn) },
		},
		&mygo.MenuItem{
			Type:    mygo.MenuItemCheckbox,
			Label:   "显示读写速度",
			Checked: speedOn,
			Click:   func(*mygo.MenuItem, *mygo.Window) { traySetDiskShown("speed", !speedOn) },
		},
		mygo.Separator(),
		&mygo.MenuItem{
			Label: "退出晴谷",
			Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() },
		},
	)
	return items
}

// trayDiskItems is the menu's disk section: each disk's free space, then
// the machine's live read and write rate. The rows are disabled labels —
// the menu is a readout; the switches below it decide what shows and the
// 磁盘健康 page is where the disks are worked on.
func trayDiskItems(rep diskhealth.Report, capOn, speedOn bool) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	if capOn {
		rows := 0
		for _, d := range rep.Disks {
			if d.Total <= 0 || rows >= trayDiskMaxRows {
				continue
			}
			rows++
			items = append(items, &mygo.MenuItem{
				Label: fmt.Sprintf("%s — 可用 %s / 共 %s（%d%%）",
					diskTrayName(d), formatBytes(d.Free), formatBytes(d.Total),
					int64(d.UsedRatio()*100+0.5)),
				Disabled: true,
			})
		}
		if len(items) == 0 {
			// No probe has landed yet, or no disk reported a size: say
			// which, rather than a section that silently is not there.
			label := "磁盘容量：读取中…"
			if diskhealth.Ready() {
				label = "磁盘容量：未报告"
			}
			items = append(items, &mygo.MenuItem{Label: label, Disabled: true})
		}
	}
	if speedOn {
		var read, write float64
		known := false
		for _, d := range rep.Disks {
			if d.RateKnown {
				read += d.ReadBps
				write += d.WriteBps
				known = true
			}
		}
		label := "磁盘读写：采样中…"
		if known {
			label = fmt.Sprintf("磁盘读写：读 %s/s · 写 %s/s",
				trayRate(read), trayRate(write))
		}
		items = append(items, &mygo.MenuItem{Label: label, Disabled: true})
	}
	items = append(items, &mygo.MenuItem{
		Label: "刷新磁盘信息",
		Click: func(*mygo.MenuItem, *mygo.Window) {
			diskhealth.Refresh()
			// The forced probe lands in the background; the menu picks it
			// up in trayRebuild when it does.
		},
	})
	return items
}

// diskTrayName is the disk's menu label: the model when there is one
// people recognize, else the system's own name for it.
func diskTrayName(d diskhealth.Disk) string {
	if d.Model != "" {
		return d.Model
	}
	return d.ID
}

// trayRate formats a bytes-per-second figure for a menu row, the status
// bar's byte labels scaled to speeds.
func trayRate(bps float64) string {
	switch {
	case bps >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", bps/1024/1024/1024)
	case bps >= 1024*1024:
		return fmt.Sprintf("%.1f MB", bps/1024/1024)
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB", bps/1024)
	default:
		return fmt.Sprintf("%.0f B", bps)
	}
}

// traySettings reads the settings the tray's menu depends on: a live
// window's store when one exists (the in-memory copy every window shares
// its writes with), the state file otherwise — a tray-only run, every
// window closed, still answers from what was last saved.
func traySettings() appSettings {
	appsMu.Lock()
	var st *persistedState
	for _, a := range apps {
		st = a.store
		break
	}
	appsMu.Unlock()
	if st == nil {
		return loadState().settings()
	}
	return st.settings()
}

// trayShown is the two menu switches as one value, so a caller can keep
// or pass the pair.
type trayShown struct {
	capOn   bool
	speedOn bool
}

// trayDiskShown maps the settings to the two menu switches. Both default
// to on, stored as their negations so an older state file reads as the
// new default (statestore.go's appSettings).
func trayDiskShown(s appSettings) trayShown {
	return trayShown{capOn: !s.NoTrayDiskCap, speedOn: !s.NoTrayDiskSpeed}
}

// traySetDiskShown flips one of the two switches and rebuilds the menu.
// The click arrives on the main thread, where the settings write and the
// menu swap belong.
func traySetDiskShown(which string, on bool) {
	set := func(s *appSettings) {
		switch which {
		case "cap":
			s.NoTrayDiskCap = !on
		case "speed":
			s.NoTrayDiskSpeed = !on
		}
	}
	appsMu.Lock()
	var app *App
	for _, a := range apps {
		app = a
		break
	}
	appsMu.Unlock()
	if app != nil {
		// A live app propagates the change to every open window at once,
		// the settings dialog's own write-through.
		app.applySettings(set)
	} else {
		loadState().updateSettings(set)
	}
	trayDiskWatch()
	trayRebuild()
}

// trayDiskWatch starts the tray's disk probe while any disk information
// shows, and stops it when none does: a menu asked to stay quiet costs
// nothing. The probe itself is shared through diskhealth's cache with the
// 磁盘健康 page's own watcher, so two watchers still probe once.
func trayDiskWatch() {
	shown := trayDiskShown(traySettings())
	if shown.capOn || shown.speedOn {
		if trayDiskStop == nil {
			trayDiskStop = diskhealth.Watch()
		}
	} else if trayDiskStop != nil {
		trayDiskStop()
		trayDiskStop = nil
	}
}

// trayRebuild swaps in a menu built from the freshest probe and settings.
// It runs wherever diskUpdateNotify delivers it — the main thread of the
// running app — and does nothing before setupTray has planted the tray.
func trayRebuild() {
	if tray == nil {
		return
	}
	tray.SetMenu(mygo.NewMenu(trayMenuItems(diskhealth.ReadAsync(), traySettings())))
}

// diskUpdateMu and friends fan diskhealth's single process-wide update
// callback out to everyone who wants to hear a probe land: the windows
// repaint (main.go installs the notify on the first frame), and the tray
// rebuilds its menu. One callback slot, many listeners.
var (
	diskUpdateMu  sync.Mutex
	diskUpdateFns []func()
)

// diskUpdateOn adds fn to the listeners a landing probe runs, returning
// its removal.
func diskUpdateOn(fn func()) (off func()) {
	diskUpdateMu.Lock()
	diskUpdateFns = append(diskUpdateFns, fn)
	i := len(diskUpdateFns) - 1
	diskUpdateMu.Unlock()
	return func() {
		diskUpdateMu.Lock()
		if i < len(diskUpdateFns) {
			diskUpdateFns = append(diskUpdateFns[:i], diskUpdateFns[i+1:]...)
		}
		diskUpdateMu.Unlock()
	}
}

// diskUpdateNotify is the one callback diskhealth holds: it runs the
// listeners on the main thread of the running app — a probe lands on its
// own goroutine, and the tray's menu swap wants the main thread — or
// right there when no app runs, as the headless tests do.
func diskUpdateNotify() {
	diskUpdateMu.Lock()
	fns := append([]func(){}, diskUpdateFns...)
	diskUpdateMu.Unlock()
	run := func() {
		for _, fn := range fns {
			fn()
		}
	}
	if mygo.App.IsReady() {
		mygo.RunOnMain(run)
	} else {
		run()
	}
}

// trayMainApp brings back the window a tray action acts on: the first
// live one is restored, shown and focused, and with none left — the user
// closed every window while the process kept running in the tray — a new
// one opens, as main does on launch. The tray's click and menu callbacks
// fire on the main thread, where the window calls below run directly.
func trayMainApp() *App {
	appsMu.Lock()
	var app *App
	for _, a := range apps {
		if a.win != nil && !a.win.IsDestroyed() {
			app = a
			break
		}
	}
	appsMu.Unlock()
	if app == nil {
		// main's launch sequence: the window rides its own App, which the
		// registry drops when the window closes.
		app = newApp()
		registerApp(app)
		win := newAppWindow(app)
		win.OnClosed(func() { unregisterApp(app) })
		return app
	}
	if app.win.IsMinimized() {
		app.win.Restore()
	}
	app.win.Show()
	app.win.Focus()
	return app
}

// trayShowMainWindow is the menu's 显示晴谷: bring a window back.
func trayShowMainWindow() { trayMainApp() }

// trayNewWindow is the menu's 新建窗口: a fresh one over the home folder,
// as Ctrl+N does in a window — over the same store a live window uses, so
// the pins and recents stay one collection; a tray-only run, no window
// left, loads its own.
func trayNewWindow() {
	appsMu.Lock()
	var store *persistedState
	for _, a := range apps {
		store = a.store
		break
	}
	appsMu.Unlock()
	var app *App
	if store != nil {
		app = newAppWithStore(store)
	} else {
		app = newApp()
	}
	registerApp(app)
	win := newAppWindow(app)
	win.OnClosed(func() { unregisterApp(app) })
	win.Show()
	win.Focus()
}

// closeToTray reports whether closing a window should hide it in the
// tray instead of ending the process. A quit in progress, or a run whose
// tray never came up, still closes: otherwise the window would vanish
// with no way back and no way to quit.
func closeToTray(quitting, hasTray bool) bool {
	return hasTray && !quitting
}

// hideWindowToTray handles a close. It returns true when the close was
// turned into a hide, so the caller cancels the real close.
func hideWindowToTray(win *mygo.Window) bool {
	if !closeToTray(appQuitting, tray != nil) || win == nil {
		return false
	}
	minimizeToTray(win)
	return true
}

// minimizeToTray keeps the process in the tray: the window hides instead
// of docking in the taskbar or exiting, and the first time a notification
// says where it went. OnMinimize and a cancelled close both fire it on
// the main thread.
func minimizeToTray(win *mygo.Window) {
	if win == nil {
		return
	}
	win.Hide()
	if trayNotified {
		return
	}
	trayNotified = true
	// The note needs the running app, which headless runs never have; a
	// desktop's refusal to show it is no reason to fail the hide, so the
	// error is dropped either way.
	if !mygo.App.IsReady() {
		return
	}
	_ = mygo.NewNotification(mygo.NotificationOptions{
		Title: "晴谷",
		Body:  "晴谷在后台运行，点击托盘图标可恢复窗口",
	}).Show()
}
