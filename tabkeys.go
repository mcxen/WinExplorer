package main

import "github.com/egoist/mygo/ui"

// tabkeys.go is the tab strip's keyboard: Explorer's and the browsers'
// tab shortcuts, with the macOS spellings beside them. Cmd+[ and Cmd+]
// stay 后退/前进 as Finder has them, so the macOS tab switch is
// Cmd+Shift+[ / ], as in Safari and Finder.

// handleTabKeys runs the tab shortcuts and reports whether one fired.
func (a *App) handleTabKeys(c *ui.Context) bool {
	n := len(a.tabs)
	switch {
	case c.Shortcut(ui.Ctrl, ui.KeyT), c.Shortcut(ui.Cmd, ui.KeyT):
		a.openTab(a.cur().path)
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyT), c.Shortcut(ui.Cmd|ui.Shift, ui.KeyT):
		a.reopenClosedTab()
	case c.Shortcut(ui.Ctrl, ui.KeyW), c.Shortcut(ui.Cmd, ui.KeyW), c.Shortcut(ui.Ctrl, ui.KeyF4):
		a.closeTab(a.activeTab)
	case c.Shortcut(ui.Ctrl, ui.KeyTab), c.Shortcut(ui.Ctrl, ui.KeyPageDown),
		c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketRight):
		a.switchTo((a.activeTab + 1) % n)
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab), c.Shortcut(ui.Ctrl, ui.KeyPageUp),
		c.Shortcut(ui.Cmd|ui.Shift, ui.KeyBracketLeft):
		a.switchTo((a.activeTab + n - 1) % n)
	default:
		// Ctrl/Cmd+1..8 goes to that tab, +9 to the last, as Explorer's.
		digits := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4,
			ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
		for i, key := range digits {
			if !c.Shortcut(ui.Ctrl, key) && !c.Shortcut(ui.Cmd, key) {
				continue
			}
			if key == ui.Key9 {
				i = n - 1
			}
			if i < n {
				a.switchTo(i)
			}
			return true
		}
		return false
	}
	return true
}
