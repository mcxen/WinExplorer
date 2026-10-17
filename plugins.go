package main

import (
	"slices"
	"sync"
)

// plugins.go is the registry of Sunvale's built-in plugins: features that
// ship with the app but that the user can turn off, in the settings
// dialog's 插件 section. Each feature registers itself with RegisterPlugin
// — its hooks say where it appears: context menus, the 查看更多 menu, the
// sidebar, the preview pane's metadata, its own rows in the 插件 section.
// The collectors below gather only the enabled plugins' contributions, so
// a switched-off plugin stops appearing or animating. The store records
// only the turned-off ids (statestore.go), so a store that records
// nothing has every plugin on: a fresh install behaves as today's app
// does.
//
// init registers the built-ins in the order the 插件 section lists them,
// each registration living in the plugin's own file.

// Plugin is one built-in plugin: what the settings dialog lists and what
// the collectors call each frame. Every hook is optional — a plugin with
// no hooks still gets its switch in the 插件 section.
type Plugin struct {
	ID   string
	Name string
	Desc string

	// OnEnable runs the moment the plugin is switched on — remote, for
	// example, starts its LAN server back when the account still has it
	// enabled. nil skips.
	OnEnable func(a *App)
	// OnDisable runs the moment the plugin is switched off — remote stops
	// its server and running GIF animations end. nil skips.
	OnDisable func(a *App)

	// MenuItems contributes context-menu entries. The pane background's
	// menu passes an empty sel; a row's menu passes the selection. Return
	// nil to stay out of that menu.
	MenuItems func(a *App, sel []Entry) []PluginMenuItem
	// MoreItems contributes entries to the command bar's 查看更多 menu.
	MoreItems func(a *App) []PluginMenuItem
	// NavItems contributes sidebar places, after 主文件夹, in order.
	NavItems func(a *App) []PluginNavItem
	// MetaRows contributes rows to the preview pane's and 属性 dialog's
	// metadata sections for the single selected entry.
	MetaRows func(a *App, e Entry) []PluginMetaRow
	// Settings declares the plugin's own rows for the settings dialog's
	// 插件 section, nested under its switch while it is on — the
	// per-plugin preferences pluginGet/pluginSet persist. nil means the
	// plugin has no settings of its own.
	Settings func(a *App) []PluginSetting
}

// PluginMenuItem is one menu entry a plugin contributes. Disabled greys it
// out, Checked shows the check mark, Run fires on the click.
type PluginMenuItem struct {
	Label    string
	Disabled bool
	Checked  bool
	Run      func()
}

// PluginNavItem is one sidebar place a plugin contributes. Kind is the
// place icon's kind, as the built-in places use; Path is the navigation
// target, which also selects the row and accepts drops.
type PluginNavItem struct {
	Key   string
	Label string
	Kind  string
	Path  string
	Run   func()
}

// PluginMetaRow is one metadata row a plugin contributes. Copy renders a
// 复制 button beside the value that puts it on the clipboard.
type PluginMetaRow struct {
	Key  string
	Val  string
	Copy bool
}

// PluginSettingKind is which row shape one declared plugin setting uses.
type PluginSettingKind int

const (
	PluginSettingSwitch PluginSettingKind = iota // a toggle
	PluginSettingSelect                          // a dropdown
	PluginSettingInfo                            // a read-only value line
	PluginSettingAction                          // a button
)

// PluginSetting is one row a plugin declares for the settings dialog's
// 插件 section. The plugin only declares data — the dialog renders every
// row with the same shapes its own sections use, so the section keeps one
// look whichever plugin the rows came from — and writes changes through
// the hooks, pluginGet/pluginSet persisting them.
type PluginSetting struct {
	Kind PluginSettingKind
	Name string
	Desc string

	// Switch: On is the toggle's state; Change receives "on" or "off".
	// Select: Value is the current option, Options the choices; Change
	// receives the option the user picked. Info: Value is the line shown
	// beside the name.
	On      bool
	Value   string
	Options []string
	Change  func(val string)

	// Action: Action is the button's label; Run fires on the click.
	Action string
	Run    func()
}

var (
	pluginMu       sync.RWMutex
	pluginRegistry []*Plugin
)

// init registers the built-in plugins, in the order the settings dialog's
// 插件 section lists them. Each register function lives in its plugin's
// own file.
func init() {
	registerRemotePlugin()
	registerGalleryPlugin()
	registerGifPlugin()
	registerHashPlugin()
	registerDupsPlugin()
	registerUsagePlugin()
	registerDiskPlugin()
}

// RegisterPlugin adds p to the registry. A duplicate ID is a programmer
// error, as is registering after the UI has read the registry — panic.
func RegisterPlugin(p Plugin) {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	for _, q := range pluginRegistry {
		if q.ID == p.ID {
			panic("plugin: duplicate id " + q.ID)
		}
	}
	cp := p
	pluginRegistry = append(pluginRegistry, &cp)
}

// registeredPlugins returns the registry in registration order.
func registeredPlugins() []*Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	return slices.Clone(pluginRegistry)
}

// findPlugin returns the plugin with the id, or nil.
func findPlugin(id string) *Plugin {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	for _, p := range pluginRegistry {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// pluginEnabled reports whether the app's store has the plugin on: a
// plugin is on when the store's off-list does not record it, and a nil
// store (headless tests) has every plugin on.
func (a *App) pluginEnabled(id string) bool {
	return pluginOn(a.store, id)
}

// pluginOn is pluginEnabled over any store: the off-list holds only the
// turned-off ids, so an empty one is every plugin on — first-run behavior
// is today's.
func pluginOn(st *persistedState, id string) bool {
	if st == nil {
		return true
	}
	return !slices.Contains(st.pluginList(), id)
}

// pluginSetEnabled turns the plugin on or off, persisting it and repainting
// — the immediate mode reads the new state on the next frame, as
// applySettings does for the view settings. The plugin's own hooks own the
// immediate aftermath: OnDisable stops what it was doing at once, OnEnable
// brings it back.
func (a *App) pluginSetEnabled(id string, on bool) {
	a.store.setPlugin(id, on)
	if p := findPlugin(id); p != nil {
		if on {
			if p.OnEnable != nil {
				p.OnEnable(a)
			}
		} else if p.OnDisable != nil {
			p.OnDisable(a)
		}
	}
	repaint()
}

// pluginMenuItems gathers the enabled plugins' context-menu entries for
// one menu: sel empty for the pane background, the selection for a row.
func (a *App) pluginMenuItems(sel []Entry) []PluginMenuItem {
	var out []PluginMenuItem
	for _, p := range registeredPlugins() {
		if !a.pluginEnabled(p.ID) || p.MenuItems == nil {
			continue
		}
		out = append(out, p.MenuItems(a, sel)...)
	}
	return out
}

// pluginMoreItems gathers the enabled plugins' 查看更多 menu entries.
func (a *App) pluginMoreItems() []PluginMenuItem {
	var out []PluginMenuItem
	for _, p := range registeredPlugins() {
		if !a.pluginEnabled(p.ID) || p.MoreItems == nil {
			continue
		}
		out = append(out, p.MoreItems(a)...)
	}
	return out
}

// pluginNavItems gathers the enabled plugins' sidebar places, in
// registration order.
func (a *App) pluginNavItems() []PluginNavItem {
	var out []PluginNavItem
	for _, p := range registeredPlugins() {
		if !a.pluginEnabled(p.ID) || p.NavItems == nil {
			continue
		}
		out = append(out, p.NavItems(a)...)
	}
	return out
}

// pluginMetaRows gathers the enabled plugins' metadata rows for the entry.
func (a *App) pluginMetaRows(e Entry) []PluginMetaRow {
	var out []PluginMetaRow
	for _, p := range registeredPlugins() {
		if !a.pluginEnabled(p.ID) || p.MetaRows == nil {
			continue
		}
		out = append(out, p.MetaRows(a, e)...)
	}
	return out
}

// pluginSettings gathers one plugin's declared settings rows for the
// settings dialog's 插件 section: nil when the plugin is off or declares
// none, so a disabled plugin's settings hide along with its features.
func (a *App) pluginSettings(p *Plugin) []PluginSetting {
	if p == nil || p.Settings == nil || !a.pluginEnabled(p.ID) {
		return nil
	}
	return p.Settings(a)
}

// pluginGet reads one per-plugin setting (statestore.go's PluginSettings):
// the value the plugin stored for the key, empty when never set.
func (a *App) pluginGet(id, key string) string {
	if a.store == nil {
		return ""
	}
	return a.store.pluginSetting(id, key)
}

// pluginSet stores one per-plugin setting, persisting it through the same
// path as every other state change.
func (a *App) pluginSet(id, key, val string) {
	if a.store == nil {
		return
	}
	a.store.setPluginSetting(id, key, val)
}
