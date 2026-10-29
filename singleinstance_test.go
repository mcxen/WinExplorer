package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireSingleInstance(t *testing.T) {
	cases := []struct {
		name   string
		fwd    forwardResult
		osFree int  // attempt at which the mutex becomes free; -1 never
		window bool // raise finds a window
		want   bool
		raised bool
	}{
		{"first", forwardNoOne, 0, false, true, false},
		{"second, acked", forwardAcked, 0, false, false, false},
		{"silent instance: take over", forwardNoReply, -1, true, true, false},
		{"relaunch: old process exits", forwardNoOne, 3, false, true, false},
		{"mutex held, window raised", forwardNoOne, -1, true, false, true},
		{"mutex held, no window: start", forwardNoOne, -1, false, true, true},
	}
	for _, c := range cases {
		raised, tries := false, 0
		got := acquireSingleInstance(singleInstanceDeps{
			forward: func() forwardResult { return c.fwd },
			listen:  func() error { return nil },
			osLock: func() (bool, func()) {
				free := c.osFree >= 0 && tries >= c.osFree
				tries++
				return free, func() {}
			},
			raise:   func() bool { raised = true; return c.window },
			retries: 5,
			sleep:   func() {},
		})
		if got != c.want || raised != c.raised {
			t.Errorf("%s: go on=%v raised=%v, want %v %v", c.name, got, raised, c.want, c.raised)
		}
	}
}

// TestForwardStaleAndSilent covers the real socket: a file left by a
// killed process, a listener that never acks, and one that does.
func TestForwardStaleAndSilent(t *testing.T) {
	dir, err := os.MkdirTemp("", "svsi")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	msg := instanceMsg{Args: []string{"/x"}, WorkingDir: "/"}

	// Stale: a socket file nobody listens on (kill -9).
	ln, err := listenInstance(path)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stale socket file expected")
	}
	if r := forwardToInstance(path, msg, time.Second); r != forwardNoOne {
		t.Fatalf("stale: %v", r)
	}
	if ln, err = listenInstance(path); err != nil {
		t.Fatal("take over stale:", err)
	}

	// Silent: accepts, reads, never answers.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	start := time.Now()
	if r := forwardToInstance(path, msg, 300*time.Millisecond); r != forwardNoReply {
		t.Fatalf("silent: %v", r)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("silent took %v", d)
	}
	ln.Close()

	// Acked only when a window shows.
	ln, _ = listenInstance(path)
	defer ln.Close()
	var show atomic.Bool
	show.Store(true)
	go serveInstance(ln, func(m instanceMsg) bool { return show.Load() && m.Args[0] == "/x" })
	if r := forwardToInstance(path, msg, time.Second); r != forwardAcked {
		t.Fatalf("ack: %v", r)
	}
	show.Store(false)
	if r := forwardToInstance(path, msg, time.Second); r != forwardNoReply {
		t.Fatalf("no window: %v", r)
	}
}
