package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Ctrl+N windows share the persisted store: a pin from one window's app
// is the other's too, exactly as Explorer's pins are one collection.
func TestSharedStoreAcrossApps(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-mw")
	defer os.RemoveAll(dir)
	store := &persistedState{}

	a1 := newAppWithStore(store)
	a2 := newAppWithStore(store)
	pin := filepath.Join(dir, "共享目录")
	os.MkdirAll(pin, 0o755)
	if !a1.store.pin(pin) {
		t.Fatal("pin failed")
	}
	if len(a2.pinnedPlaces()) == 0 || a2.pinnedPlaces()[0].Path != pin {
		t.Fatalf("second app does not see the pin: %v", a2.pinnedPlaces())
	}

	// Each window keeps its own navigation; the shared store does not
	// leak tab state.
	if a1.tabs[0].path != a2.tabs[0].path {
		t.Fatal("unexpected tab sharing")
	}
}

func TestAppRegistryLifecycle(t *testing.T) {
	a1 := newTestApp(t, "/tmp")
	a2 := newTestApp(t, "/tmp")
	registerApp(a1)
	registerApp(a2)
	appsMu.Lock()
	if len(apps) != 2 {
		appsMu.Unlock()
		t.Fatalf("apps = %d", len(apps))
	}
	appsMu.Unlock()
	unregisterApp(a1)
	appsMu.Lock()
	if len(apps) != 1 || apps[0] != a2 {
		appsMu.Unlock()
		t.Fatal("unregister removed the wrong app")
	}
	appsMu.Unlock()
	unregisterApp(a2)
	// Shutting down an app with no preview never panics.
	unregisterApp(newTestApp(t, "/tmp"))
}
