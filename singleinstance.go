package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
)

// singleinstance.go keeps one process per user: a second launch (the
// desktop shortcut while the first hides in the tray, the NSIS finish
// page's 运行 next to the autostart entry, an updater relaunch racing the
// old process) hands its command line to the first and exits, and the
// first shows a window.
//
// 协议（docs/specs/19-single-instance.md）：每用户一个 Unix 套接字。后启动的
// 进程发一行 JSON {args, workingDir}，运行中的实例把窗口显示到前台（没有窗口
// 就新建一个）之后才回 "ok"。拿不到 ok —— 连不上、套接字是崩溃残留、对方
// 卡住或已没有界面 —— 新进程在 forwardTimeout 内放弃转发，接管锁正常启动，
// 不会再出现“点了没反应”。mygo 自带的单实例锁读完就关连接，不等窗口，
// 所以这里不用它。
//
// On Windows a named mutex backs it (singleinstance_windows.go): it covers
// an updater relaunch while the old process is still exiting, and an
// older version that only listens on mygo's socket.

const (
	forwardDialTimeout = time.Second
	forwardTimeout     = 2 * time.Second // whole handoff, ack included
	serveShowTimeout   = 1500 * time.Millisecond
)

type instanceMsg struct {
	Args       []string `json:"args"`
	WorkingDir string   `json:"workingDir"`
}

// forwardResult is what a handoff attempt found.
type forwardResult int

const (
	forwardNoOne   forwardResult = iota // nothing listening (none, or a stale socket)
	forwardAcked                        // the running instance showed a window
	forwardNoReply                      // someone answered the dial but never acked
)

func (r forwardResult) String() string {
	return [...]string{"no-listener", "acked", "no-ack"}[r]
}

// instanceSocketPath is the per-user socket; Unix socket paths are limited
// to about 100 bytes.
func instanceSocketPath() string {
	key := "com.mcxen.sunvale\n" + strconv.Itoa(os.Getuid())
	if h, err := os.UserHomeDir(); err == nil {
		key += "\n" + h
	}
	sum := sha256.Sum256([]byte(key))
	file := "sunvale-" + hex.EncodeToString(sum[:6]) + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			return filepath.Join(dir, file)
		}
	}
	return filepath.Join(os.TempDir(), file)
}

// forwardToInstance hands msg to the instance listening on path and waits
// for its "ok" until timeout.
func forwardToInstance(path string, msg instanceMsg, timeout time.Duration) forwardResult {
	deadline := time.Now().Add(timeout)
	conn, err := net.DialTimeout("unix", path, min(forwardDialTimeout, timeout))
	if err != nil {
		return forwardNoOne
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	data, _ := json.Marshal(msg)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return forwardNoReply
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ok" {
		slog.Warn("single-instance: no ack from running instance", "reply", strings.TrimSpace(line), "err", err)
		return forwardNoReply
	}
	return forwardAcked
}

// listenInstance takes the lock: a socket file nobody answers on is left
// over from a crash (or an instance that stopped answering) and replaced.
func listenInstance(path string) (net.Listener, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	return ln, nil
}

// serveInstance answers forwarded launches: show(msg) runs the request and
// reports whether a window is visible; only then the caller gets "ok".
func serveInstance(ln net.Listener, show func(instanceMsg) bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(forwardTimeout))
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				return
			}
			var msg instanceMsg
			if json.Unmarshal([]byte(line), &msg) != nil {
				return
			}
			if show(msg) {
				_, _ = conn.Write([]byte("ok\n"))
				return
			}
			_, _ = conn.Write([]byte("nowindow\n"))
		}()
	}
}

// singleInstanceDeps are the pieces acquireSingleInstance runs on, swapped
// in tests.
type singleInstanceDeps struct {
	forward func() forwardResult
	listen  func() error                        // take the socket lock
	osLock  func() (first bool, release func()) // the OS mutex; first when none exists
	raise   func() bool                         // show the running instance's window without the socket
	retries int                                 // extra mutex attempts while a relaunched-from process exits
	sleep   func()
}

// acquireSingleInstance reports whether this process should go on. Only an
// acked handoff makes it exit: a missing, stale or silent instance is
// taken over. When the socket is ours but the mutex is taken, the previous
// process is still exiting (updater relaunch) or is an older version on
// another socket: the mutex is retried a moment, then its window raised;
// with no window to raise this launch starts anyway.
func acquireSingleInstance(d singleInstanceDeps) bool {
	tookOver := false
	for attempt := 0; attempt < 3; attempt++ {
		r := d.forward()
		slog.Info("single-instance: handoff", "result", r.String(), "attempt", attempt)
		if r == forwardAcked {
			return false
		}
		tookOver = tookOver || r == forwardNoReply
		err := d.listen()
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.Errno(10048)) {
			slog.Warn("single-instance: socket lock unavailable, starting without it", "err", err)
			break
		}
		d.sleep()
	}
	if tookOver {
		// The silent instance is not ours to wait for.
		slog.Warn("single-instance: running instance did not ack, took over")
		return true
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
	if d.raise() {
		slog.Info("single-instance: mutex held, raised the other window")
		return false
	}
	slog.Warn("single-instance: mutex held but no window found, starting anyway")
	return true
}

var (
	instanceReady   = make(chan struct{})
	instanceReadyMu sync.Once
	instanceLn      net.Listener
)

// installSingleInstance runs in main before any window: false means exit.
func installSingleInstance() bool {
	path := instanceSocketPath()
	wd, _ := os.Getwd()
	msg := instanceMsg{Args: os.Args[1:], WorkingDir: wd}
	ok := acquireSingleInstance(singleInstanceDeps{
		forward: func() forwardResult { return forwardToInstance(path, msg, forwardTimeout) },
		listen: func() error {
			ln, err := listenInstance(path)
			instanceLn = ln
			return err
		},
		osLock:  osInstanceLock,
		raise:   raiseRunningInstance,
		retries: 30,
		sleep:   func() { time.Sleep(100 * time.Millisecond) },
	})
	if !ok {
		slog.Info("single-instance: handed over to the running instance, exiting")
		return false
	}
	if ln := instanceLn; ln != nil {
		st, _ := os.Stat(path)
		go serveInstance(ln, showForwarded)
		// Release before the windows go: a launch during shutdown then
		// starts fresh instead of talking to a process on its way out.
		release := func() {
			ln.Close()
			if fi, err := os.Stat(path); err == nil && st != nil && os.SameFile(fi, st) {
				_ = os.Remove(path)
			}
		}
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { release() })
		mygo.App.OnQuit(release)
	}
	mygo.App.WhenReady(func() { instanceReadyMu.Do(func() { close(instanceReady) }) })
	// macOS: the Dock icon (or Finder opening the app again) re-activates
	// the running process; with every window closed or hidden in the tray
	// one comes back.
	mygo.App.OnActivate(func(hasVisible bool) {
		if !hasVisible && !appQuitting {
			slog.Info("single-instance: reopen with no visible window, showing one")
			trayMainApp()
		}
	})
	slog.Info("single-instance: lock acquired", "socket", path)
	return true
}

// showForwarded runs on the listener's goroutine: the window work goes to
// the main thread (never inside a view, viewguard.go), and the answer waits
// until a window is shown.
func showForwarded(msg instanceMsg) bool {
	select {
	case <-instanceReady:
	case <-time.After(serveShowTimeout):
		slog.Warn("single-instance: forwarded launch before ready")
		return false
	}
	if appQuitting {
		return false
	}
	done := make(chan bool, 1)
	mygo.RunOnMain(func() {
		app := trayMainApp()
		if targets := cliTargets(msg.Args, msg.WorkingDir); len(targets) > 0 {
			app.openCLITargets(targets, false)
			app.win.Invalidate()
		}
		done <- app.win != nil && !app.win.IsDestroyed()
	})
	select {
	case ok := <-done:
		slog.Info("single-instance: forwarded launch handled", "args", len(msg.Args), "window", ok)
		return ok
	case <-time.After(serveShowTimeout):
		slog.Warn("single-instance: main thread busy, forwarded launch not acked")
		return false
	}
}
