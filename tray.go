package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"

	"github.com/egoist/mygo"
	xdraw "golang.org/x/image/draw"
)

// trayIconRaw is the app icon as it ships, 1024×1024; the tray wants the
// same picture small.
//
//go:embed resources/icon.png
var trayIconRaw []byte

// tray is the process's one tray icon, so a second setupTray hands back the
// one that already exists instead of planting another icon.
var tray *mygo.Tray

// trayNotified reports the minimized-to-tray note has been shown: however
// many windows minimize to the tray, the notification comes once per run.
var trayNotified bool

// trayIconSide is the tray icon's edge in pixels: mygo suggests 16×16 points,
// and 32 covers Retina's 2×.
const trayIconSide = 32

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

// setupTray plants the tray icon with the two things a tray entry is for:
// bringing the window back and quitting. It is called in App.WhenReady, the
// point NewTray's after-ready contract means, so calling it before then
// would panic.
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
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{
				Label: "显示晴谷",
				Click: func(*mygo.MenuItem, *mygo.Window) { trayShowMainWindow() },
			},
			mygo.Separator(),
			{
				Label: "退出晴谷",
				Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() },
			},
		}),
	})
	if err != nil {
		return nil, err
	}
	// A left click restores the window where clicks open the tray menu
	// anyway (macOS); Windows keeps the menu on the right button.
	t.OnClick(func() { trayShowMainWindow() })
	tray = t
	return tray, nil
}

// trayShowMainWindow brings a window back from the tray: the first live one
// is restored, shown and focused, and with none left — the user closed every
// window while the process kept running in the tray — a new one opens, as
// main does on launch. The tray's click and menu callbacks fire it on the
// main thread, where the window calls below run directly.
func trayShowMainWindow() {
	var win *mygo.Window
	appsMu.Lock()
	for _, a := range apps {
		if a.win != nil && !a.win.IsDestroyed() {
			win = a.win
			break
		}
	}
	appsMu.Unlock()
	if win == nil {
		// main's launch sequence: the window rides its own App, which the
		// registry drops when the window closes.
		app := newApp()
		registerApp(app)
		win = newAppWindow(app)
		win.OnClosed(func() { unregisterApp(app) })
		return
	}
	if win.IsMinimized() {
		win.Restore()
	}
	win.Show()
	win.Focus()
}

// minimizeToTray is the minimize that keeps the process in the tray: the
// window hides instead of docking in the taskbar, and the first time a
// notification says where it went, so a window gone from the taskbar is not
// mistaken for a quit. OnMinimize fires it on the main thread.
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
		Body:  "晴谷已最小化到托盘，点击托盘图标可恢复窗口",
	}).Show()
}
