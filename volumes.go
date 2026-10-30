package main

import (
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// volumes.go caches the volumes 此电脑 lists. sysVolumes reads /media and
// /run/media on Linux, stats every mount on macOS and asks the shell for
// every drive's label on Windows; the sidebar asks for the list every frame
// it draws the tree, so a per-frame read would put that I/O on the UI thread.
// The cache serves what the last read found and refreshes in the background
// once it is stale, outside view construction.

const volumeCacheTTL = 5 * time.Second

// readVolumes is a seam for tests; production reads the OS's list.
var readVolumes = sysVolumes

var volumeCache struct {
	mu      sync.Mutex
	places  []Place
	at      time.Time
	loading bool
}

// warmVolumes reads the volumes once, off the view path: a fresh window must
// have 此电脑's rows in its first frame. Called from app creation, which is
// not view construction except when a new window opens from a menu — there
// the cache is already warm, and a stale one refreshes in the background.
func warmVolumes() { _ = volumePlaces() }

// volumePlaces is the cached volume list. Outside a view a stale cache is
// refreshed before returning; inside one the stale list is served and a
// background read replaces it, so the sidebar never blocks on a mount.
func volumePlaces() []Place {
	volumeCache.mu.Lock()
	cached := slices.Clone(volumeCache.places)
	fresh := len(cached) > 0 && time.Since(volumeCache.at) < volumeCacheTTL
	loading := volumeCache.loading
	volumeCache.mu.Unlock()
	if fresh {
		return cached
	}
	if inView.Load() > 0 {
		if !loading {
			loadVolumesAsync()
		}
		return cached
	}
	return loadVolumesNow()
}

// loadVolumesNow reads the volumes on the caller's goroutine. It is only
// called outside view construction.
func loadVolumesNow() []Place {
	places := slices.Clone(readVolumes())
	volumeCache.mu.Lock()
	volumeCache.places = places
	volumeCache.at = time.Now()
	volumeCache.loading = false
	volumeCache.mu.Unlock()
	return slices.Clone(places)
}

// loadVolumesAsync replaces the cache off the UI thread and asks the windows
// to redraw; a window sitting on 此电脑 rebuilds its rows from the new list.
func loadVolumesAsync() {
	volumeCache.mu.Lock()
	if volumeCache.loading {
		volumeCache.mu.Unlock()
		return
	}
	volumeCache.loading = true
	volumeCache.mu.Unlock()
	go func() {
		places := slices.Clone(readVolumes())
		volumeCache.mu.Lock()
		volumeCache.places = places
		volumeCache.at = time.Now()
		volumeCache.loading = false
		volumeCache.mu.Unlock()
		repaint()
		notifyVolumesChanged()
	}()
}

// notifyVolumesChanged reloads 此电脑 in every window showing it. The tree
// reads the cache on its next frame; a file page needs its rows rebuilt.
func notifyVolumesChanged() {
	appsMu.Lock()
	all := slices.Clone(apps)
	appsMu.Unlock()
	if !mygo.App.IsReady() {
		return
	}
	for _, app := range all {
		app.postAssist(func() {
			if app.cur().path == pathThisPC {
				app.reload()
			}
		})
	}
}
