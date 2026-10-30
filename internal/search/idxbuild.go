package search

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// idxbuild.go builds an index file the way updatedb (mlocate) does: a folder
// whose mtime equals the old snapshot's reuses the old rows without being
// listed; only changed folders are read again. Every folder is still stat'd,
// which is what makes a refresh cost "one stat per folder" instead of "one
// stat per file".

// excludedNames are folder names never worth indexing: tool caches and VCS
// internals that hold most of a developer disk's files and none of its
// documents.
var excludedNames = map[string]bool{
	"node_modules": true, ".git": true, ".hg": true, ".svn": true,
	"__pycache__": true, ".cache": true, ".gradle": true, ".m2": true,
	".npm": true, ".Trash": true,
}

// bnode is one entry of the tree a build assembles before writing it out.
type bnode struct {
	name  string
	flags uint16
	size  int64
	mod   int64
	kids  []*bnode
}

type buildStats struct {
	entries   int
	dirs      int
	reused    int // folders whose rows came from the old snapshot
	rescanned int // folders read from disk
	cut       bool
}

type builder struct {
	ctx      context.Context
	old      *snapshot
	max      int
	progress *atomic.Int64
	throttle time.Duration
	skip     map[string]bool
	onDirty  func(string)
	stats    buildStats
}

// build walks roots into a tree. A cut build (cancelled, timed out or capped)
// returns what it has with stats.cut set.
func (b *builder) build(roots []string) []*bnode {
	b.skip = map[string]bool{}
	for _, p := range platformPrune() {
		b.skip[p] = true
	}
	for _, p := range networkMounts() {
		b.skip[p] = true
	}
	var out []*bnode
	for _, root := range roots {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			continue
		}
		n := &bnode{name: root, flags: flagDir, mod: st.ModTime().Unix()}
		oldID, hasOld := b.old.lookup(root)
		b.fill(root, n, oldID, hasOld)
		out = append(out, n)
		b.skip[root] = true // a later volume root does not walk it again
	}
	return out
}

func (b *builder) stop() bool {
	if b.ctx.Err() != nil || b.stats.entries >= b.max {
		b.stats.cut = true
		return true
	}
	return false
}

func (b *builder) add(n *bnode) {
	b.stats.entries++
	b.progress.Add(1)
}

func (b *builder) fill(path string, n *bnode, oldID uint32, hasOld bool) {
	if b.stop() {
		return
	}
	b.stats.dirs++
	if b.throttle > 0 && b.stats.dirs%256 == 0 {
		time.Sleep(b.throttle)
	}
	hidden := n.flags & flagHidden
	if hasOld && b.old.isDir(oldID) && b.old.mod(oldID) == n.mod {
		b.stats.reused++
		first, count := b.old.kids(oldID)
		n.kids = make([]*bnode, 0, count)
		for c := first; c < first+count; c++ {
			if b.stop() {
				return
			}
			k := &bnode{
				name: string(b.old.nameBytes(c)), flags: b.old.flags(c),
				size: b.old.size(c), mod: b.old.mod(c),
			}
			b.add(k)
			n.kids = append(n.kids, k)
			if k.flags&flagDir == 0 {
				continue
			}
			kp := joinPath(path, k.name)
			st, err := os.Lstat(kp)
			if err != nil || !st.IsDir() {
				continue // a symlinked folder, or gone since: rows only
			}
			k.mod = st.ModTime().Unix()
			b.fill(kp, k, c, true)
		}
		return
	}
	b.stats.rescanned++
	if hasOld && b.onDirty != nil {
		b.onDirty(path)
	}
	des, err := os.ReadDir(path)
	if err != nil {
		return
	}
	n.kids = make([]*bnode, 0, len(des))
	for _, de := range des {
		if b.stop() {
			return
		}
		name := de.Name()
		kp := joinPath(path, name)
		if de.IsDir() && (excludedNames[name] || b.skip[kp]) {
			continue
		}
		k := &bnode{name: name, flags: hidden}
		if strings.HasPrefix(name, ".") {
			k.flags |= flagHidden
		}
		if info, err := de.Info(); err == nil {
			k.size, k.mod = info.Size(), info.ModTime().Unix()
		}
		real := de.IsDir()
		if real {
			k.flags |= flagDir
			k.size = 0
		} else if de.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(kp); err == nil && st.IsDir() {
				k.flags |= flagDir
			}
		}
		b.add(k)
		n.kids = append(n.kids, k)
		if real {
			var cid uint32
			var ok bool
			if hasOld {
				cid, ok = b.old.child(oldID, name)
			}
			b.fill(kp, k, cid, ok)
		}
	}
}

func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, string(filepath.Separator)) {
		return dir + name
	}
	return dir + string(filepath.Separator) + name
}

// encode lays the tree out breadth-first and builds the trigram postings.
func encode(roots []*bnode, built time.Time, complete bool) []byte {
	order := append([]*bnode(nil), roots...)
	parents := make([]uint32, len(roots))
	for i := range parents {
		parents[i] = noParent
	}
	firsts := make([]uint32, 0, len(roots))
	for i := 0; i < len(order); i++ {
		firsts = append(firsts, uint32(len(order)))
		for _, k := range order[i].kids {
			order = append(order, k)
			parents = append(parents, uint32(i))
		}
	}
	n := len(order)
	recs := make([]byte, n*recSize)
	var names []byte
	for i, e := range order {
		r := recs[i*recSize:]
		le.PutUint32(r[rParent:], parents[i])
		le.PutUint32(r[rName:], uint32(len(names)))
		le.PutUint16(r[rLen:], uint16(len(e.name)))
		le.PutUint16(r[rFlags:], e.flags)
		le.PutUint32(r[rFirst:], firsts[i])
		le.PutUint32(r[rCount:], uint32(len(e.kids)))
		le.PutUint64(r[rSize:], uint64(e.size))
		le.PutUint64(r[rMod:], uint64(e.mod))
		names = append(names, e.name...)
		order[i] = nil // let the tree go as it is written
	}
	tri, post := encodePostings(recs, names, len(roots), n)
	head := make([]byte, headerSize)
	copy(head, idxMagic)
	le.PutUint64(head[8:], uint64(built.Unix()))
	if complete {
		le.PutUint32(head[16:], 1)
	}
	le.PutUint32(head[20:], uint32(n))
	le.PutUint32(head[24:], uint32(len(roots)))
	le.PutUint32(head[28:], uint32(len(names)))
	le.PutUint32(head[32:], uint32(len(tri)/triSize))
	le.PutUint32(head[36:], uint32(len(post)))
	out := make([]byte, 0, headerSize+len(recs)+len(names)+len(tri)+len(post))
	out = append(out, head...)
	out = append(out, recs...)
	out = append(out, names...)
	out = append(out, tri...)
	return append(out, post...)
}

// writeIndex replaces path atomically: a reader mapping the old file keeps
// its pages until it unmaps.
func writeIndex(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// encodePostings builds the trigram table in two counting passes instead of
// sorting (trigram, id) pairs: ids are visited in order, so every list comes
// out sorted for delta encoding.
func encodePostings(recs, names []byte, roots, n int) (tri, post []byte) {
	nameOf := func(i int) string {
		r := recs[i*recSize:]
		off, l := le.Uint32(r[rName:]), uint32(le.Uint16(r[rLen:]))
		return strings.ToLower(string(names[off : off+l]))
	}
	counts := map[uint32]uint32{}
	var tris []uint32
	for i := roots; i < n; i++ {
		tris = trigramsOf(nameOf(i), tris)
		for _, t := range tris {
			counts[t]++
		}
	}
	keys := make([]uint32, 0, len(counts))
	for t := range counts {
		keys = append(keys, t)
	}
	slices.Sort(keys)
	starts := make(map[uint32]uint32, len(keys))
	var total uint32
	for _, t := range keys {
		starts[t] = total
		total += counts[t]
	}
	ids := make([]uint32, total)
	for i := roots; i < n; i++ {
		tris = trigramsOf(nameOf(i), tris)
		for _, t := range tris {
			ids[starts[t]] = uint32(i)
			starts[t]++
		}
	}
	var buf [binary.MaxVarintLen32]byte
	tri = make([]byte, 0, len(keys)*triSize)
	var at uint32
	for _, t := range keys {
		cnt := counts[t]
		off := len(post)
		var prev uint32
		for _, id := range ids[at : at+cnt] {
			post = append(post, buf[:binary.PutUvarint(buf[:], uint64(id-prev))]...)
			prev = id
		}
		at += cnt
		var row [triSize]byte
		le.PutUint32(row[0:], t)
		le.PutUint32(row[4:], uint32(off))
		le.PutUint32(row[8:], cnt)
		tri = append(tri, row[:]...)
	}
	return tri, post
}
