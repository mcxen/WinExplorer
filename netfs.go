package main

import (
	"sync"
	"time"
)

// netfs.go decides whether a path lives on a network mount, which is what
// lets the listing and the thumbnails treat it as the slow, far-away
// filesystem it is: metadata is read in parallel, folder-size walks and
// eager thumbnail decoding wait until something asks for them. The answer
// only ever chooses a speed strategy — a wrong guess costs a little time,
// never correctness — so the platform probes below err toward "local".

// netfsTTL is how long a decision is kept. A mount does not change type
// under a running app, but the path may be remounted while the window
// stays open; a few seconds absorbs the per-frame asks without pinning a
// stale answer for long.
const netfsTTL = 5 * time.Second

type netfsVerdict struct {
	network bool
	at      time.Time
}

var netfsCache sync.Map // map[string]netfsVerdict

// isNetworkPath reports whether path sits on a network mount — SMB, NFS,
// AFP, WebDAV and their kin. Results are cached for netfsTTL.
func isNetworkPath(path string) bool {
	if path == "" {
		return false
	}
	if v, ok := netfsCache.Load(path); ok {
		if verdict := v.(netfsVerdict); time.Since(verdict.at) < netfsTTL {
			return verdict.network
		}
	}
	network := platformIsNetworkPath(path)
	netfsCache.Store(path, netfsVerdict{network: network, at: time.Now()})
	return network
}

// forgetNetworkPath drops a cached verdict, for a test or a remount that
// should be re-probed at once.
func forgetNetworkPath(path string) { netfsCache.Delete(path) }
