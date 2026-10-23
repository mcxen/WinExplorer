package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"
)

// singleinstance.go keeps one process per user: a second launch (the
// desktop shortcut while the first hides in the tray, the NSIS finish
// page's 运行 next to the autostart entry, an updater relaunch racing the
// old process) hands its command line to the first and exits, and the
// first brings its window to the front.
//
// mygo's lock is a per-user Unix socket. On Windows a named mutex backs it
// (singleinstance_windows.go): when the socket cannot be made (AF_UNIX
// unavailable, temp dir trouble) mygo would say "first instance", and the
// mutex still knows better.

// singleInstanceDeps are the pieces acquireSingleInstance runs on, swapped
// in tests.
type singleInstanceDeps struct {
	osLock     func() (first bool, release func()) // the OS mutex; first when none exists
	socketLock func() bool                         // mygo.App.RequestSingleInstanceLock
	raise      func()                              // bring the running instance forward without the socket
	retries    int                                 // extra mutex attempts while a relaunched-from process exits
	sleep      func()
}

// acquireSingleInstance reports whether this process should go on. The
// socket answers first: a running instance that hears us is definitely
// there. When the socket is ours but the mutex is taken, either the socket
// is broken on this machine or the previous process is still exiting after
// an updater relaunch (mygo releases the socket at quit, the mutex goes
// with the process), so the mutex is retried for a moment before this
// launch gives way and raises the other window.
func acquireSingleInstance(d singleInstanceDeps) bool {
	if !d.socketLock() {
		return false
	}
	for try := 0; ; try++ {
		first, release := d.osLock()
		if first {
			return true
		}
		if release != nil {
			release()
		}
		if try >= d.retries {
			break
		}
		d.sleep()
	}
	d.raise()
	return false
}

// installSingleInstance runs in main before any window: false means exit.
func installSingleInstance() bool {
	ok := acquireSingleInstance(singleInstanceDeps{
		osLock:     osInstanceLock,
		socketLock: mygo.App.RequestSingleInstanceLock,
		raise:      raiseRunningInstance,
		retries:    30,
		sleep:      func() { time.Sleep(100 * time.Millisecond) },
	})
	if !ok {
		log.Printf("晴谷已在运行，已唤起已有窗口")
		return false
	}
	// Another launch: bring a window back from the tray and to the front.
	// An archive on its command line opens through OnOpenFile as usual.
	mygo.App.OnSecondInstance(func([]string, string) {
		if mygo.App.IsReady() {
			trayMainApp()
		}
	})
	return true
}
