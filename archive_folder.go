package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mholt/archives"
)

// archive_folder.go makes an archive look like a folder. It owns the path
// split that finds the archive inside a virtual path, a header-only index
// shared between listings, the lister that turns that index into rows, and
// the one-entry extractor the UI uses when an item is copied out. Reading
// contents only happens on extraction; walking the index stops at headers.

// errArchiveNoFolder is what a virtual path naming a directory the archive
// does not contain reports, so the pane shows a short Chinese message.
var errArchiveNoFolder = errors.New("压缩包中没有该文件夹")

// archiveSplit finds the first component of p that is an existing regular
// file with an archive name. Everything after it is the slash-separated
// path inside the archive. A directory component is never an archive: a
// macOS .app package keeps behaving like the folder it is.
func archiveSplit(p string) (arc, inner string, ok bool) {
	if p == "" {
		return "", "", false
	}
	// Names alone rule most paths out, before any stat: an ordinary folder
	// has no component that looks like an archive.
	hasArchiveName := false
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if isArchive(part) {
			hasArchiveName = true
			break
		}
	}
	if !hasArchiveName {
		return "", "", false
	}

	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	root := vol
	if strings.HasPrefix(rest, "/") || strings.HasPrefix(rest, "\\") {
		root = vol + string(filepath.Separator)
	}
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
	cur := root
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Stat(cur)
		if err != nil {
			continue
		}
		if info.IsDir() || !info.Mode().IsRegular() || !isArchive(part) {
			continue
		}
		return cur, strings.Join(parts[i+1:], "/"), true
	}
	return "", "", false
}

// archiveNode is one member of an index. Path is the normalized slash form
// used as its key; Name is the last component the row shows.
type archiveNode struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// archiveIndex is the archive's directory tree, built from headers only.
// nodes keeps the path lookup and children gives the listing its rows.
type archiveIndex struct {
	nodes    map[string]*archiveNode
	children map[string][]*archiveNode
}

// ensure returns the node at p, creating it as an implicit directory when
// an archive omits the parent header (many producers write only files).
func (idx *archiveIndex) ensure(p string) *archiveNode {
	if n := idx.nodes[p]; n != nil {
		return n
	}
	name := p
	parent := ""
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		name, parent = p[i+1:], p[:i]
	}
	n := &archiveNode{Name: name, Path: p, IsDir: true}
	idx.nodes[p] = n
	idx.children[parent] = append(idx.children[parent], n)
	return n
}

// add records a member, first materializing any parents the archive left
// implicit, and updates a directory that an earlier member only implied.
func (idx *archiveIndex) add(name string, isDir bool, size int64, mod time.Time) {
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		idx.ensure(strings.Join(parts[:i], "/"))
	}
	n := idx.nodes[name]
	if n == nil {
		parent := ""
		if len(parts) > 1 {
			parent = strings.Join(parts[:len(parts)-1], "/")
		}
		n = &archiveNode{Name: parts[len(parts)-1], Path: name}
		idx.nodes[name] = n
		idx.children[parent] = append(idx.children[parent], n)
	}
	n.IsDir = isDir
	n.Size = size
	n.ModTime = mod
}

// archiveMemberName normalizes a header's name and says whether it is a
// directory. Escapes are dropped: their path would leave the index's root.
func archiveMemberName(raw string, isDir bool) (string, bool) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	isDir = isDir || strings.HasSuffix(raw, "/")
	raw = strings.TrimLeft(raw, "/")
	for strings.HasPrefix(raw, "./") {
		raw = raw[2:]
	}
	name := path.Clean(raw)
	if name == "." || name == "" || name == ".." || strings.HasPrefix(name, "../") {
		return "", false
	}
	return name, isDir
}

// cleanArchiveInner normalizes the part of a virtual path below the
// archive. ok is false for a path trying to climb out.
func cleanArchiveInner(inner string) (string, bool) {
	inner = strings.ReplaceAll(inner, "\\", "/")
	inner = strings.Trim(inner, "/")
	inner = path.Clean(inner)
	switch {
	case inner == ".":
		return "", true
	case inner == ".." || strings.HasPrefix(inner, "../"):
		return "", false
	default:
		return inner, true
	}
}

type archiveIndexKey struct {
	path string
	size int64
	mod  time.Time
}

const archiveIndexCacheMax = 16

var (
	archiveIndexMu    sync.Mutex
	archiveIndexCache = map[archiveIndexKey]*archiveIndex{}
)

// archiveIndexFor returns the cached index for arc, rebuilding it when the
// file's size or modification time changed. The build happens outside the
// lock; a duplicate build is cheaper than holding every listing behind a
// slow 7z header read.
func archiveIndexFor(arc string) (*archiveIndex, error) {
	info, err := os.Stat(arc)
	if err != nil {
		return nil, err
	}
	key := archiveIndexKey{path: filepath.Clean(arc), size: info.Size(), mod: info.ModTime()}
	archiveIndexMu.Lock()
	if idx := archiveIndexCache[key]; idx != nil {
		archiveIndexMu.Unlock()
		return idx, nil
	}
	archiveIndexMu.Unlock()

	idx, err := buildArchiveIndex(arc)
	if err != nil {
		return nil, err
	}

	archiveIndexMu.Lock()
	defer archiveIndexMu.Unlock()
	for k := range archiveIndexCache {
		if k.path == key.path && k != key {
			delete(archiveIndexCache, k)
		}
	}
	if len(archiveIndexCache) >= archiveIndexCacheMax {
		if _, exists := archiveIndexCache[key]; !exists {
			for k := range archiveIndexCache {
				delete(archiveIndexCache, k)
				break
			}
		}
	}
	archiveIndexCache[key] = idx
	return idx, nil
}

// buildArchiveIndex walks the archive's headers and records every member.
// The callback never calls Open, so no file body is read.
func buildArchiveIndex(arc string) (*archiveIndex, error) {
	f, err := os.Open(arc)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ctx := context.Background()
	format, stream, err := archives.Identify(ctx, filepath.Base(arc), f)
	if err != nil {
		return nil, err
	}
	ex, ok := format.(archives.Extractor)
	if !ok {
		return nil, fmt.Errorf("不支持的压缩格式")
	}
	idx := &archiveIndex{
		nodes:    map[string]*archiveNode{},
		children: map[string][]*archiveNode{},
	}
	err = ex.Extract(ctx, stream, func(_ context.Context, info archives.FileInfo) error {
		raw := info.NameInArchive
		if raw == "" && info.FileInfo != nil {
			raw = info.Name()
		}
		name, isDir := archiveMemberName(raw, info.IsDir())
		if name == "" {
			return nil
		}
		var size int64
		var mod time.Time
		if info.FileInfo != nil {
			size = info.Size()
			mod = info.ModTime()
		}
		if isDir {
			size = 0
		}
		idx.add(name, isDir, size, mod)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// archiveLister lists a folder inside an archive. Every listed path keeps
// the archive in it, so the rest of the app can navigate and act on an
// item without knowing it lived compressed a moment ago.
type archiveLister struct{}

func (archiveLister) List(dir string) ([]Entry, error) {
	arc, inner, ok := archiveSplit(dir)
	if !ok {
		return nil, errArchiveNoFolder
	}
	idx, err := archiveIndexFor(arc)
	if err != nil {
		return nil, err
	}
	inner, ok = cleanArchiveInner(inner)
	if !ok {
		return nil, errArchiveNoFolder
	}
	if inner != "" {
		n := idx.nodes[inner]
		if n == nil || !n.IsDir {
			return nil, errArchiveNoFolder
		}
	}
	kids := idx.children[inner]
	entries := make([]Entry, 0, len(kids))
	for _, n := range kids {
		e := Entry{
			Name:    n.Name,
			Path:    filepath.Join(arc, filepath.FromSlash(n.Path)),
			IsDir:   n.IsDir,
			Size:    n.Size,
			ModTime: n.ModTime,
		}
		if !e.IsDir {
			e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(n.Name), "."))
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// archivePathWithin reports whether target stays at or below root, the second
// half of the zip-slip defense after the member name was normalized.
func archivePathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// extractArchiveEntry writes one file, or the whole subtree under a
// directory, into destDir. The entry's base name roots the output, so a
// folder 子目录 keeps its structure below 子目录 rather than spilling its
// children into destDir. The returned path is the file or folder written.
func extractArchiveEntry(arc, inner, destDir string) (string, error) {
	name, ok := cleanArchiveInner(inner)
	if !ok {
		return "", errArchiveNoFolder
	}
	idx, err := archiveIndexFor(arc)
	if err != nil {
		return "", err
	}
	node := idx.nodes[name]
	if node == nil {
		return "", errArchiveNoFolder
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(destDir, node.Name)
	if !archivePathWithin(destDir, out) {
		return "", errors.New("压缩包中的路径不安全")
	}
	if node.IsDir {
		if err := os.MkdirAll(out, 0o755); err != nil {
			return "", err
		}
	} else if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}

	f, err := os.Open(arc)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ctx := context.Background()
	format, stream, err := archives.Identify(ctx, filepath.Base(arc), f)
	if err != nil {
		return "", err
	}
	ex, ok := format.(archives.Extractor)
	if !ok {
		return "", fmt.Errorf("不支持的压缩格式")
	}

	err = ex.Extract(ctx, stream, func(_ context.Context, info archives.FileInfo) error {
		raw := info.NameInArchive
		if raw == "" && info.FileInfo != nil {
			raw = info.Name()
		}
		member, isDir := archiveMemberName(raw, info.IsDir())
		if member == "" {
			return nil
		}
		rel := ""
		if node.IsDir {
			if member != name && !strings.HasPrefix(member, name+"/") {
				return nil
			}
			if member != name {
				rel = member[len(name)+1:]
			}
		} else if member != name {
			return nil
		}

		target := out
		if node.IsDir {
			target = filepath.Join(out, filepath.FromSlash(rel))
		}
		if !archivePathWithin(out, target) {
			return errors.New("压缩包中的路径不安全")
		}
		if isDir {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := info.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, rc)
		closeErr := w.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return out, nil
}
