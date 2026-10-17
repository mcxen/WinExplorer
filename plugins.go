package main

import (
	"slices"
	"sync/atomic"
)

// plugins.go is the registry of Sunvale's built-in plugins: features that
// ship with the app but that the user can turn off, in the settings
// dialog's 插件 section. The registry below is what that section lists, in
// this fixed order, and what each feature checks at its entry —
// pluginEnabled — so a switched-off plugin stops appearing or animating.
// The store records only the turned-off ids (statestore.go), so a store
// that records nothing has every plugin on: a fresh install behaves as
// today's app does.

// plugin is one built-in plugin the settings dialog lists.
type plugin struct {
	ID   string
	Name string
	Desc string
}

// plugins are the built-in plugins, in the order the 插件 section lists
// them.
var plugins = []plugin{
	{"remote", "远程访问", "把主文件夹通过局域网共享给手机"},
	{"gallery", "画廊", "侧边栏的图片画廊页"},
	{"gif", "GIF 自动播放", "预览窗格与图标视图播放 GIF 动画"},
	{"hash", "文件哈希", "预览窗格显示文件的 SHA-256"},
	{"dups", "重复文件查找", "在当前文件夹查找重复文件"},
	{"usage", "可视化查看占用", "右键用矩形树图查看文件夹占用情况"},
}

// pluginStore is the persisted store package-level callers read — the gif
// player starts from a preview's frame, where there is no App at hand.
// newAppWithStore installs the shared store; a nil one has every plugin
// on, as the frames before it and the headless tests see.
var pluginStore atomic.Pointer[persistedState]

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

// pluginSet turns the plugin on or off, persisting it and repainting — the
// immediate mode reads the new state on the next frame, as applySettings
// does for the view settings. Switching a plugin off also stops what it is
// doing at once: the remote server shuts down and running GIF animations
// end, their static frames staying.
func (a *App) pluginSet(id string, on bool) {
	a.store.setPlugin(id, on)
	switch id {
	case "remote":
		// The plugin gates the LAN server the … menu's 远程访问 starts;
		// the account's own Enabled switch stays the menu's. Turning the
		// plugin off stops the running server, turning it on brings it
		// back when the account still has remote access on.
		if !on {
			a.remoteStop()
		} else if a.remoteConf().Enabled {
			a.remoteStart()
		}
	case "gif":
		if !on {
			stopGifPlay()
		}
	}
	repaint()
}
