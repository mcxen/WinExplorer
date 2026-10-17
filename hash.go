package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// hash.go is the 文件哈希 plugin: the preview pane's metadata section
// gains a SHA-256 row for the single selected file. The digest is
// computed in a background goroutine and cached by path + size + modtime,
// evicted oldest first, so re-selecting a file or reopening the pane does
// not re-read it, and a file that changed on disk hashes again.

// hashPluginID is the plugin's id in the registry (plugins.go).
const hashPluginID = "hash"

// hashMaxEntries bounds the digest cache; recomputing a dropped digest is
// one file read, an unbounded map is not.
const hashMaxEntries = 64

// hashFacts is one computed digest, as the pane's row shows it and as the
// cache holds it for a key. Err is the message when hashing failed —
// failures are cached too, so a file that cannot be read does not get
// re-attempted every frame.
type hashFacts struct {
	Path string
	Sum  string
	Err  string
}

// hashCacheKey names one digest: the same path at a new size or modtime
// is a different file for hashing purposes.
type hashCacheKey struct {
	path string
	size int64
	mod  time.Time
}

var (
	hashMu    sync.Mutex
	hashCache = map[hashCacheKey]*hashFacts{}
	hashOrder []hashCacheKey
	hashBusy  = map[hashCacheKey]bool{}

	// hashComputes counts digests actually computed, for the tests.
	hashComputes atomic.Int64
	// hashState is the latest computed digest the pane's row can show.
	hashState atomic.Pointer[hashFacts]
)

// requestHash starts the digest of path in the background unless the file
// is gone or a directory, the facts are already current, or a compute for
// it is in flight; the callers gate the plugin off. Safe to call from the
// render path on every frame; the repeat calls fall through to the checks
// here.
func (a *App) requestHash(path string) {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return
	}
	key := hashCacheKey{path: path, size: info.Size(), mod: info.ModTime()}
	hashMu.Lock()
	_, cached := hashCache[key]
	busy := hashBusy[key]
	if !cached && !busy {
		hashBusy[key] = true
	}
	hashMu.Unlock()
	if cached || busy {
		return
	}
	go a.loadHash(key)
}

// loadHash computes the digest, caches it, hands it to the pane and asks
// for a repaint so the row shows it.
func (a *App) loadHash(key hashCacheKey) {
	hashComputes.Add(1)
	sum, err := fileSHA256(key.path)
	facts := &hashFacts{Path: key.path, Sum: sum}
	if err != nil {
		facts.Err = "无法计算哈希：" + err.Error()
	}
	hashMu.Lock()
	delete(hashBusy, key)
	hashCache[key] = facts
	hashOrder = append(hashOrder, key)
	hashTrimLocked()
	hashMu.Unlock()
	hashState.Store(facts)
	repaint()
}

// hashTrimLocked drops the oldest cached digests past the cap. The caller
// holds hashMu.
func hashTrimLocked() {
	for len(hashCache) > hashMaxEntries && len(hashOrder) > 0 {
		oldest := hashOrder[0]
		hashOrder = hashOrder[1:]
		delete(hashCache, oldest)
	}
}

// hashRowValue is what the pane's SHA-256 row shows for path: the value,
// and whether it is a real digest a 复制 button can put on the clipboard.
// The cache has the word for the file as it is now; while it computes or
// after the cache dropped it, the latest facts stand in.
func hashRowValue(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	key := hashCacheKey{path: path, size: info.Size(), mod: info.ModTime()}
	hashMu.Lock()
	defer hashMu.Unlock()
	if e, ok := hashCache[key]; ok {
		if e.Err != "" {
			return e.Err, false
		}
		return e.Sum, true
	}
	if hashBusy[key] {
		return "计算中…", false
	}
	if f := hashState.Load(); f != nil && f.Path == path {
		if f.Err != "" {
			return f.Err, false
		}
		return f.Sum, true
	}
	return "未计算", false
}

// fileSHA256 is the digest of a file's contents, streamed.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
