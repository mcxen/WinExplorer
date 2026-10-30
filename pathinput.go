package main

// pathinput.go is the address bar's text side, apart from its look: how
// what the user typed becomes a place to go (quotes, ~, environment
// variables, relative names, a file that opens its folder), and the
// completions offered while typing, read from a cache of folder names
// that a background read fills so a keystroke never waits on the disk.

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// pathSeps are the characters that end a folder name as typed. Windows
// takes both slashes; elsewhere a backslash is an ordinary character of a
// name, so only '/' separates.
func pathSeps() string {
	if runtime.GOOS == "windows" {
		return `/\`
	}
	return "/"
}

func isPathSep(r byte) bool { return strings.IndexByte(pathSeps(), r) >= 0 }

// normalizePathInput tidies typed or pasted text into a path: surrounding
// space and one pair of matching quotes go (Explorer's 复制为路径 quotes
// paths), a leading ~ is the home folder, and environment variables
// expand — %VAR% on Windows, $VAR and ${VAR} elsewhere. A variable that is
// not set is left as typed, so the error names what the user wrote.
func normalizePathInput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	s = strings.TrimPrefix(s, "file://")
	if s == "~" || strings.HasPrefix(s, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(s, `~\`)) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			s = home + s[1:]
		}
	}
	if runtime.GOOS == "windows" {
		s = expandPercentEnv(s)
	} else if strings.Contains(s, "$") {
		s = os.Expand(s, func(name string) string {
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			return "${" + name + "}"
		})
	}
	return s
}

// expandPercentEnv expands cmd.exe's %VAR% references that are set.
func expandPercentEnv(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if v, ok := os.LookupEnv(name); ok && name != "" {
			b.WriteString(s[:i])
			b.WriteString(v)
			s = s[i+2+j:]
			continue
		}
		b.WriteString(s[:i+1])
		s = s[i+1:]
	}
	b.WriteString(s)
	return b.String()
}

// specialPageFor maps the names the address bar shows for the app's own
// pages back to them.
func specialPageFor(s string) (string, bool) {
	switch s {
	case "", "此电脑", pathThisPC:
		return pathThisPC, true
	case "主文件夹", pathHome:
		return pathHome, true
	case "回收站", pathTrash:
		return pathTrash, true
	case "画廊", pathGallery:
		return pathGallery, true
	case "磁盘健康", pathDisks:
		return pathDisks, true
	}
	return "", false
}

// absPathInput makes a normalized input absolute: a relative name is taken
// in base, the folder on show, as Explorer's address bar does, and a bare
// drive ("D:") is that drive's root.
func absPathInput(s, base string) string {
	if runtime.GOOS == "windows" && len(s) == 2 && s[1] == ':' {
		return s + `\`
	}
	if filepath.IsAbs(s) || base == "" || strings.HasPrefix(base, "::{") {
		return s
	}
	return filepath.Join(base, s)
}

// pathTarget is where an address entry leads: a folder (or page) to show,
// and the file to select in it when a file was typed.
type pathTarget struct {
	dir, file string
}

// resolvePathInput turns the address bar's text into a target, or the
// message the bar shows under itself when there is nowhere to go.
func resolvePathInput(text, base string) (pathTarget, string) {
	s := normalizePathInput(text)
	if p, ok := specialPageFor(s); ok {
		return pathTarget{dir: p}, ""
	}
	p := filepath.Clean(absPathInput(s, base))
	// A path may name an archive or a member inside it: stat cannot see
	// through the archive, so the index answers instead.
	if arc, inner, ok := archiveSplit(p); ok {
		name, clean := cleanArchiveInner(inner)
		if !clean {
			return pathTarget{}, "找不到“" + s + "”。请检查拼写后重试。"
		}
		if name == "" {
			return pathTarget{dir: p}, ""
		}
		idx, err := archiveIndexFor(arc)
		if err != nil || idx.nodes[name] == nil {
			return pathTarget{}, "找不到“" + s + "”。请检查拼写后重试。"
		}
		if idx.nodes[name].IsDir {
			return pathTarget{dir: p}, ""
		}
		return pathTarget{dir: filepath.Dir(p), file: p}, ""
	}
	info, err := os.Stat(p)
	switch {
	case err != nil:
		return pathTarget{}, "找不到“" + s + "”。请检查拼写后重试。"
	case info.IsDir():
		return pathTarget{dir: p}, ""
	}
	return pathTarget{dir: filepath.Dir(p), file: p}, ""
}

// splitForCompletion splits the text into the folder typed so far, as
// written, and the partial name after its last separator. ok is false
// while no separator was typed: there is no folder to list yet.
func splitForCompletion(text string) (parent, partial string, ok bool) {
	t := strings.TrimLeft(text, " \"'")
	for i := len(t) - 1; i >= 0; i-- {
		if isPathSep(t[i]) {
			return t[:i+1], t[i+1:], true
		}
	}
	return "", t, false
}

// matchNames orders the names starting with partial, ignoring case, before
// those only containing it, each alphabetically, up to limit.
func matchNames(names []string, partial string, limit int) []string {
	p := strings.ToLower(partial)
	var pre, mid []string
	for _, n := range names {
		l := strings.ToLower(n)
		switch {
		case strings.HasPrefix(l, p):
			pre = append(pre, n)
		case p != "" && strings.Contains(l, p):
			mid = append(mid, n)
		}
	}
	byLower := func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }
	slices.SortFunc(pre, byLower)
	slices.SortFunc(mid, byLower)
	out := append(pre, mid...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// dirNames is one folder's subfolders as a background read found them.
type dirNames struct {
	names  []string // subfolder names, hidden ones included
	hidden map[string]bool
	ready  bool
	err    error
	at     time.Time
}

// folderNameCache holds recent folder reads for completion and the
// breadcrumb menus. A read older than folderNameTTL is served while a
// fresh one runs, so the list never blanks while typing.
type folderNameCache struct {
	mu      sync.Mutex
	entries map[string]*dirNames
	notify  func()
}

const (
	folderNameTTL = 3 * time.Second
	// folderNameCap bounds a read of a huge folder: completion lists a
	// dozen names, and a menu of thousands is no use either.
	folderNameCap = 5000
)

var folderNames = &folderNameCache{entries: map[string]*dirNames{}, notify: repaint}

// get returns what is known of dir's subfolders, and starts a read when
// nothing or only a stale list is known.
func (fc *folderNameCache) get(dir string) dirNames {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	d := fc.entries[dir]
	if d == nil {
		d = &dirNames{}
		fc.entries[dir] = d
		fc.load(dir)
	} else if d.ready && time.Since(d.at) > folderNameTTL {
		d.at = time.Now() // one refresh at a time
		fc.load(dir)
	}
	return *d
}

// load reads dir off the caller's goroutine; fc.mu is held.
func (fc *folderNameCache) load(dir string) {
	go func() {
		names, hidden, err := readFolderNames(dir)
		fc.mu.Lock()
		fc.entries[dir] = &dirNames{names: names, hidden: hidden, ready: true, err: err, at: time.Now()}
		if len(fc.entries) > 64 {
			for k, v := range fc.entries {
				if k != dir && v.ready {
					delete(fc.entries, k)
					break
				}
			}
		}
		notify := fc.notify
		fc.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
}

// fresh reads dir on the caller and replaces what the cache held: the tree
// wants the listing as it is, not the completion cache's few-second-old copy.
func (fc *folderNameCache) fresh(dir string) dirNames {
	names, hidden, err := readFolderNames(dir)
	d := dirNames{names: names, hidden: hidden, ready: true, err: err, at: time.Now()}
	fc.mu.Lock()
	fc.entries[dir] = &d
	fc.mu.Unlock()
	return d
}

// now returns dir's subfolders, reading them on the caller when the cache
// has none yet: a menu that opens on a click shows them at once.
func (fc *folderNameCache) now(dir string) dirNames {
	if d := fc.get(dir); d.ready {
		return d
	}
	names, hidden, err := readFolderNames(dir)
	return dirNames{names: names, hidden: hidden, ready: true, err: err, at: time.Now()}
}

// forget drops what is known of dir: its listing changed.
func (fc *folderNameCache) forget(dir string) {
	fc.mu.Lock()
	delete(fc.entries, dir)
	fc.mu.Unlock()
}

// forgetAll drops every read: 刷新 asks for the world as it is now.
func (fc *folderNameCache) forgetAll() {
	fc.mu.Lock()
	fc.entries = map[string]*dirNames{}
	fc.mu.Unlock()
}

// wait blocks until dir's read is in, for tests.
func (fc *folderNameCache) wait(dir string) dirNames {
	for i := 0; i < 400; i++ {
		if d := fc.get(dir); d.ready {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fc.get(dir)
}

// readFolderNames lists dir's subfolders without a stat per entry, except
// for links, which count when they lead to a folder.
func readFolderNames(dir string) ([]string, map[string]bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	ents, err := f.ReadDir(folderNameCap)
	if err != nil && len(ents) == 0 {
		return nil, nil, err
	}
	var names []string
	hidden := map[string]bool{}
	for _, e := range ents {
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		names = append(names, e.Name())
		if sysHiddenName(dir, e) {
			hidden[e.Name()] = true
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return names, hidden, nil
}

// visibleNames drops hidden folders unless the setting shows them.
func (d dirNames) visibleNames(showHidden bool) []string {
	if showHidden {
		return d.names
	}
	out := make([]string, 0, len(d.names))
	for _, n := range d.names {
		if !d.hidden[n] {
			out = append(out, n)
		}
	}
	return out
}

// pathSuggestion is one row of the address bar's dropdown.
type pathSuggestion struct {
	label  string // what the row shows
	detail string // the full path, muted, for places
	path   string // where choosing the row goes
	insert string // the text accepting it with Tab puts in the bar
	place  bool   // a pinned or recent place rather than a subfolder
}

const maxPathSuggestions = 12

// pathSuggestions computes the dropdown for text. Once a folder is typed
// with a separator after it, its subfolders that match the rest are
// offered. Otherwise, and when no subfolder matches, the pinned and recent
// places matching the text are. pending reports a folder read still out.
func (a *App) pathSuggestions(text string) (out []pathSuggestion, pending bool) {
	parent, partial, ok := splitForCompletion(text)
	if ok {
		dir := filepath.Clean(absPathInput(normalizePathInput(parent), a.cur().path))
		d := folderNames.get(dir)
		pending = !d.ready
		for _, n := range matchNames(d.visibleNames(a.showHidden), partial, maxPathSuggestions) {
			out = append(out, pathSuggestion{
				label: n, path: filepath.Join(dir, n), insert: parent + n,
			})
		}
		if len(out) > 0 || pending {
			return out, pending
		}
	}
	return a.placeSuggestions(text), pending
}

// placeSuggestions are the pinned folders, then the recently visited ones,
// whose name or path contains the text; all of them for empty text.
func (a *App) placeSuggestions(text string) []pathSuggestion {
	q := strings.ToLower(normalizePathInput(text))
	seen := map[string]bool{a.cur().path: true}
	var out []pathSuggestion
	add := func(name, path string) {
		if seen[path] || len(out) >= maxPathSuggestions {
			return
		}
		if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(path), q) {
			return
		}
		seen[path] = true
		out = append(out, pathSuggestion{label: name, detail: path, path: path, insert: path, place: true})
	}
	for _, p := range a.pinnedPlaces() {
		if p.Kind != "file" {
			add(p.Name, p.Path)
		}
	}
	known := knownFolderNames()
	for _, dir := range a.store.visitList(maxVisited) {
		name := filepath.Base(dir)
		if n, ok := known[dir]; ok {
			name = n
		}
		add(name, dir)
	}
	return out
}
