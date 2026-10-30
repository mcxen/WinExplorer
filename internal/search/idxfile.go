package search

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// idxfile.go is the on-disk index format (docs/specs/23-search-index.md):
// a header, a fixed-width record per entry in breadth-first order (children
// of a folder are contiguous), the names blob, a sorted trigram table and the
// delta-varint posting lists. A snapshot reads it in place — mmap where the
// platform has it — so the heap holds no per-entry rows.

const (
	idxMagic   = "SVIX0003"
	headerSize = 48
	recSize    = 36
	triSize    = 12
	noParent   = ^uint32(0)
)

const (
	flagDir    = 1 << 0
	flagHidden = 1 << 1 // the name, or a folder above it, starts with a dot
)

// record field offsets inside one recSize row.
const (
	rParent = 0  // u32
	rName   = 4  // u32 offset into names
	rLen    = 8  // u16
	rFlags  = 10 // u16
	rFirst  = 12 // u32 first child id
	rCount  = 16 // u32 child count
	rSize   = 20 // i64
	rMod    = 28 // i64 unix seconds
)

var le = binary.LittleEndian

// snapshot is one immutable, loaded index file.
type snapshot struct {
	data     []byte
	release  func()
	built    int64
	complete bool
	n        int
	roots    int
	recs     []byte
	names    []byte
	tri      []byte
	post     []byte
}

func (s *snapshot) close() {
	if s != nil && s.release != nil {
		s.release()
		s.release = nil
	}
}

func (s *snapshot) rec(id uint32) []byte {
	o := int(id) * recSize
	return s.recs[o : o+recSize]
}

func (s *snapshot) parent(id uint32) uint32 { return le.Uint32(s.rec(id)[rParent:]) }
func (s *snapshot) flags(id uint32) uint16  { return le.Uint16(s.rec(id)[rFlags:]) }
func (s *snapshot) isDir(id uint32) bool    { return s.flags(id)&flagDir != 0 }
func (s *snapshot) size(id uint32) int64    { return int64(le.Uint64(s.rec(id)[rSize:])) }
func (s *snapshot) mod(id uint32) int64     { return int64(le.Uint64(s.rec(id)[rMod:])) }

func (s *snapshot) kids(id uint32) (first, count uint32) {
	r := s.rec(id)
	return le.Uint32(r[rFirst:]), le.Uint32(r[rCount:])
}

// nameBytes aliases the file; callers copy before keeping it.
func (s *snapshot) nameBytes(id uint32) []byte {
	r := s.rec(id)
	off, n := le.Uint32(r[rName:]), uint32(le.Uint16(r[rLen:]))
	return s.names[off : off+n]
}

// child finds name among id's children, which are sorted by name.
func (s *snapshot) child(id uint32, name string) (uint32, bool) {
	first, count := s.kids(id)
	lo, hi := uint32(0), count
	for lo < hi {
		mid := (lo + hi) / 2
		c := strings.Compare(string(s.nameBytes(first+mid)), name)
		if c == 0 {
			return first + mid, true
		}
		if c < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return 0, false
}

// lookup resolves a path to its entry id. Root entries hold full paths.
func (s *snapshot) lookup(path string) (uint32, bool) {
	if s == nil {
		return 0, false
	}
	for r := 0; r < s.roots; r++ {
		root := string(s.nameBytes(uint32(r)))
		if !underRoot(path, root) {
			continue
		}
		id := uint32(r)
		rest := strings.TrimPrefix(strings.TrimPrefix(path, root), string(filepath.Separator))
		ok := true
		for rest != "" && ok {
			part, tail, _ := strings.Cut(rest, string(filepath.Separator))
			id, ok = s.child(id, part)
			rest = tail
		}
		if ok {
			return id, true
		}
	}
	return 0, false
}

// postings decodes one trigram's id list, or nil when no name has it.
func (s *snapshot) postings(t uint32) []uint32 {
	n := len(s.tri) / triSize
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		v := le.Uint32(s.tri[mid*triSize:])
		if v == t {
			off := le.Uint32(s.tri[mid*triSize+4:])
			cnt := le.Uint32(s.tri[mid*triSize+8:])
			return decodePostings(s.post[off:], int(cnt))
		}
		if v < t {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return nil
}

func decodePostings(b []byte, cnt int) []uint32 {
	out := make([]uint32, 0, cnt)
	var prev uint32
	for len(out) < cnt {
		d, w := binary.Uvarint(b)
		if w <= 0 {
			break
		}
		b = b[w:]
		prev += uint32(d)
		out = append(out, prev)
	}
	return out
}

// trigramsOf lists the distinct 3-byte windows of lowered, packed in a u32.
func trigramsOf(lowered string, into []uint32) []uint32 {
	into = into[:0]
	for i := 0; i+3 <= len(lowered); i++ {
		t := uint32(lowered[i])<<16 | uint32(lowered[i+1])<<8 | uint32(lowered[i+2])
		if !slices.Contains(into, t) {
			into = append(into, t)
		}
	}
	return into
}

var errBadIndex = errors.New("search index: bad file")

// parseSnapshot validates the header and slices the sections.
func parseSnapshot(data []byte) (*snapshot, error) {
	if len(data) < headerSize || string(data[:8]) != idxMagic {
		return nil, errBadIndex
	}
	s := &snapshot{data: data}
	s.built = int64(le.Uint64(data[8:]))
	s.complete = le.Uint32(data[16:])&1 != 0
	s.n = int(le.Uint32(data[20:]))
	s.roots = int(le.Uint32(data[24:]))
	namesLen := int(le.Uint32(data[28:]))
	nTri := int(le.Uint32(data[32:]))
	postLen := int(le.Uint32(data[36:]))
	o := headerSize
	need := o + s.n*recSize + namesLen + nTri*triSize + postLen
	if need != len(data) || s.roots > s.n {
		return nil, errBadIndex
	}
	s.recs = data[o : o+s.n*recSize]
	o += s.n * recSize
	s.names = data[o : o+namesLen]
	o += namesLen
	s.tri = data[o : o+nTri*triSize]
	o += nTri * triSize
	s.post = data[o:]
	return s, nil
}

// openSnapshot maps (or reads) path. A missing or foreign file is an error
// the caller treats as "no index yet".
func openSnapshot(path string) (*snapshot, error) {
	data, release, err := mapFile(path)
	if err != nil {
		return nil, err
	}
	s, err := parseSnapshot(data)
	if err != nil {
		release()
		return nil, err
	}
	s.release = release
	return s, nil
}

// fileSize is the index's footprint on disk, 0 when absent.
func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}
