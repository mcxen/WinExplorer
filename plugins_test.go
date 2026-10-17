package main

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestPluginDefaultsAllEnabled checks the fresh store records nothing: a
// nil plugins list is every registry plugin on, first-run behavior being
// today's. A nil store is every plugin on too.
func TestPluginDefaultsAllEnabled(t *testing.T) {
	st := &persistedState{}
	if list := st.pluginList(); list != nil {
		t.Fatalf("pluginList = %v, want nil", list)
	}
	for _, p := range plugins {
		if !pluginOn(st, p.ID) {
			t.Fatalf("pluginOn(%q) = false on a fresh store", p.ID)
		}
	}
	if !pluginOn(nil, "gif") {
		t.Fatal("pluginOn(nil) = false")
	}
}

// TestPluginSetRoundTrip turns a plugin off and back on, going through the
// state.json a fresh loadState reads. HOME and XDG_CONFIG_HOME move to
// temp directories for the test, so the round trip stays out of the
// developer's own state.
func TestPluginSetRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())

	a.store.setPlugin("dups", false)
	if a.pluginEnabled("dups") {
		t.Fatal("dups still on after setPlugin off")
	}
	// setPlugin writes through saveLocked, so the reloaded store keeps it
	// off, and only the off ids are recorded.
	st := loadState()
	if pluginOn(st, "dups") {
		t.Fatal("reloaded store has dups on")
	}
	if got := st.pluginList(); len(got) != 1 || got[0] != "dups" {
		t.Fatalf("pluginList = %v, want [dups]", got)
	}
	// Turning it back on drops the record; with every plugin on the list
	// is nil again, and the next loadState sees that.
	st.setPlugin("dups", true)
	if !pluginOn(st, "dups") {
		t.Fatal("dups still off after setPlugin on")
	}
	if list := st.pluginList(); list != nil {
		t.Fatalf("pluginList = %v, want nil", list)
	}
	again := loadState()
	if got := again.pluginList(); got != nil {
		t.Fatalf("reloaded pluginList = %v, want nil", got)
	}
	for _, p := range plugins {
		if !pluginOn(again, p.ID) {
			t.Fatalf("pluginOn(%q) = false after re-enable", p.ID)
		}
	}
	// Turning an off plugin off again does not duplicate the record.
	st.setPlugin("hash", false)
	st.setPlugin("hash", false)
	if got := st.pluginList(); len(got) != 1 || got[0] != "hash" {
		t.Fatalf("pluginList = %v, want [hash]", got)
	}
}

// TestPluginEnabledApp checks the app-level helper against its store, and
// pluginSet going through the same store. A nil store — newTestApp's apps
// have none — keeps every plugin on.
func TestPluginEnabledApp(t *testing.T) {
	a := newStoreApp(t, t.TempDir())
	for _, p := range plugins {
		if !a.pluginEnabled(p.ID) {
			t.Fatalf("pluginEnabled(%q) = false, want default on", p.ID)
		}
	}
	a.store.setPlugin("gif", false)
	if a.pluginEnabled("gif") {
		t.Fatal("pluginEnabled(gif) = true after setPlugin off")
	}
	if !a.pluginEnabled("dups") {
		t.Fatal("pluginEnabled(dups) = false, other plugins untouched")
	}
	// pluginSet persists through the same store; the headless repaint is
	// a no-op.
	a.pluginSet("dups", false)
	if a.pluginEnabled("dups") {
		t.Fatal("pluginSet did not turn dups off")
	}
	if !slices.Contains(a.store.pluginList(), "dups") {
		t.Fatal("pluginSet did not record dups")
	}
	if !(&App{}).pluginEnabled("remote") {
		t.Fatal("nil store should have every plugin on")
	}
}

// TestGifPluginGatesPlayback checks the gif player's entry: with the
// plugin off, gifFor declines, and the callers keep the decoded still.
func TestGifPluginGatesPlayback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "动画.gif")
	if err := writeAnimatedGif(path); err != nil {
		t.Fatal(err)
	}
	var off persistedState
	off.setPlugin("gif", false)
	pluginStore.Store(&off)
	defer pluginStore.Store(nil)
	if bm := gifFor(path); bm != nil {
		t.Fatal("gifFor started a player with the plugin off")
	}
}

// writeAnimatedGif writes a two-frame gif, the smallest one a player
// plays.
func writeAnimatedGif(path string) error {
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}
	g := &gif.GIF{}
	for i := range 2 {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		img.Set(i, i, pal[1])
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, g)
}
