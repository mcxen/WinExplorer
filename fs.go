package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// visibleEntries filters hidden names and the search query, then lists
// directories before files, each group ordered by the current sort.
func (a *App) visibleEntries() []Entry {
	q := strings.ToLower(strings.TrimSpace(a.cur().query))
	out := make([]Entry, 0, len(a.cur().entries))
	for _, e := range a.cur().entries {
		if !a.showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		less := entryLess(out[i], out[j], a.sortKey)
		if !a.sortAsc {
			less = entryLess(out[j], out[i], a.sortKey)
		}
		return less
	})
	return out
}

func entryLess(x, y Entry, key SortKey) bool {
	switch key {
	case SortDate:
		if !x.ModTime.Equal(y.ModTime) {
			return x.ModTime.Before(y.ModTime)
		}
	case SortType:
		if !strings.EqualFold(x.Ext, y.Ext) {
			return strings.ToLower(x.Ext) < strings.ToLower(y.Ext)
		}
	case SortSize:
		if x.Size != y.Size {
			return x.Size < y.Size
		}
	}
	return strings.ToLower(x.Name) < strings.ToLower(y.Name)
}

func (a *App) breadcrumbs() []Crumb {
	if a.cur().path == pathThisPC {
		return []Crumb{{Name: "此电脑", Path: pathThisPC}}
	}
	known := knownFolderNames()
	crumbs := []Crumb{{Name: "此电脑", Path: pathThisPC}}
	path := filepath.Clean(a.cur().path)
	vol := filepath.VolumeName(path)
	rest := path
	if vol != "" {
		rest = strings.TrimPrefix(path, vol)
		if rest == path {
			rest = ""
		}
	}
	var built string
	switch {
	case vol != "":
		built = vol
		if len(vol) == 2 && vol[1] == ':' {
			built += `\`
		}
		name := vol
		if n, ok := known[filepath.Clean(built)]; ok {
			name = n
		} else if filepath.Clean(built) == "/" || built == `\` || built == `/` {
			name = "Macintosh HD"
		}
		crumbs = append(crumbs, Crumb{Name: name, Path: filepath.Clean(built)})
	case path == "/" || strings.HasPrefix(path, "/"):
		// Unix paths have no volume name. "/" is Macintosh HD, the same
		// drive This PC lists, so Up from it returns to This PC.
		built = "/"
		crumbs = append(crumbs, Crumb{Name: "Macintosh HD", Path: "/"})
	}
	rest = strings.Trim(rest, `/\`)
	if rest == "" {
		return crumbs
	}
	for _, part := range splitPath(rest) {
		built = filepath.Join(built, part)
		name := part
		if n, ok := known[filepath.Clean(built)]; ok {
			name = n
		}
		crumbs = append(crumbs, Crumb{Name: name, Path: filepath.Clean(built)})
	}
	return crumbs
}

func splitPath(p string) []string {
	p = strings.ReplaceAll(p, `\`, "/")
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func knownFolderNames() map[string]string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	home = filepath.Clean(home)
	return map[string]string{
		home:                             "主文件夹",
		filepath.Join(home, "Desktop"):   "桌面",
		filepath.Join(home, "Downloads"): "下载",
		filepath.Join(home, "Documents"): "文档",
		filepath.Join(home, "Pictures"):  "图片",
		filepath.Join(home, "Music"):     "音乐",
		filepath.Join(home, "Videos"):    "视频",
	}
}

func (a *App) quickPlaces() []Place {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	want := []Place{
		{Name: "桌面", Path: filepath.Join(home, "Desktop"), Kind: "desktop"},
		{Name: "下载", Path: filepath.Join(home, "Downloads"), Kind: "downloads"},
		{Name: "文档", Path: filepath.Join(home, "Documents"), Kind: "documents"},
		{Name: "图片", Path: filepath.Join(home, "Pictures"), Kind: "pictures"},
		{Name: "音乐", Path: filepath.Join(home, "Music"), Kind: "music"},
		{Name: "视频", Path: filepath.Join(home, "Videos"), Kind: "videos"},
	}
	out := make([]Place, 0, len(want))
	for _, p := range want {
		info, err := os.Stat(p.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (a *App) thisPCPlaces() []Place {
	root := "/"
	places := []Place{{Name: "Macintosh HD", Path: root, Kind: "drive"}}
	vols, err := os.ReadDir("/Volumes")
	if err != nil {
		return places
	}
	rootDev := deviceID(root)
	for _, v := range vols {
		path := filepath.Join("/Volumes", v.Name())
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if same, ok := sameDevice(path, rootDev); ok && same {
			continue
		}
		places = append(places, Place{Name: v.Name(), Path: path, Kind: "drive"})
	}
	return places
}

func deviceID(path string) uint64 {
	id, ok := statDevice(path)
	if !ok {
		return 0
	}
	return id
}

func sameDevice(path string, id uint64) (same, ok bool) {
	got, ok := statDevice(path)
	if !ok || id == 0 {
		return false, false
	}
	return got == id, true
}

func statDevice(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

func cleanNav(path string) string {
	if path == pathThisPC || path == "此电脑" {
		return pathThisPC
	}
	if path == "" {
		return pathThisPC
	}
	return filepath.Clean(path)
}

func parentPath(path string) string {
	if path == pathThisPC {
		return pathThisPC
	}
	clean := filepath.Clean(path)
	if clean == "/" || clean == `\` {
		return pathThisPC
	}
	parent := filepath.Dir(clean)
	if parent == clean {
		return pathThisPC
	}
	vol := filepath.VolumeName(clean)
	if vol != "" {
		root := vol
		if len(vol) == 2 && vol[1] == ':' {
			root = vol + `\`
		}
		if filepath.Clean(clean) == filepath.Clean(root) {
			return pathThisPC
		}
	}
	if parent == "." || parent == "" {
		return pathThisPC
	}
	return parent
}
