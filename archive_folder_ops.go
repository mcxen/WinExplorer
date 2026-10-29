package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// archive_folder_ops.go wires the archive folder to the app's verbs:
// opening a member or an archive, copy-out for paste and drop, the
// 「解压到…」 command, and the native drag data for in-archive rows. The
// archive index and extractor themselves live in archive_folder.go.

// openArchiveFile extracts one member to the per-archive cache and hands
// the real file to the OS. Extraction runs off the UI thread; only the
// error is posted back.
func (a *App) openArchiveFile(arc, inner string) {
	a.statusMsg = "正在打开…"
	go func() {
		dir, err := archiveOpenCacheDir(arc)
		var out string
		if err == nil {
			out, err = extractArchiveEntry(arc, inner, dir)
		}
		if err == nil {
			err = openWithOS(out)
		}
		a.postAssist(func() {
			if err != nil {
				a.statusMsg = "无法打开：" + err.Error()
				return
			}
			if a.statusMsg == "正在打开…" {
				a.statusMsg = ""
			}
		})
	}()
}

// archiveSubtreeSize is the uncompressed total a drag would need to write
// out. Directories sum their children because a folder row's own size is 0.
func archiveSubtreeSize(idx *archiveIndex, name string) int64 {
	n := idx.nodes[name]
	if n == nil {
		return 0
	}
	if !n.IsDir {
		return n.Size
	}
	var total int64
	for _, child := range idx.children[name] {
		total += archiveSubtreeSize(idx, child.Path)
	}
	return total
}

const archiveDragMax = 64 << 20

// dragSourceDataFor is dragSourceData for rows that may live inside an
// archive. Small total sizes are materialized in the cache synchronously
// (the gesture's start, not a view frame); larger ones offer no native
// data, so the drag still works between this window's own targets.
func (a *App) dragSourceDataFor(paths []string) transfer.Data {
	type item struct {
		src, arc, inner string
	}
	var items []item
	total := int64(0)
	hasArchive := false
	for _, p := range paths {
		arc, inner, ok := archiveSplit(p)
		if !ok || inner == "" {
			items = append(items, item{src: p})
			if info, err := os.Stat(p); err == nil {
				total += info.Size()
			}
			continue
		}
		hasArchive = true
		idx, err := archiveIndexFor(arc)
		if err != nil {
			return transfer.Data{}
		}
		size := archiveSubtreeSize(idx, inner)
		if size == 0 && idx.nodes[inner] == nil {
			return transfer.Data{}
		}
		total += size
		if total > archiveDragMax {
			return transfer.Data{}
		}
		items = append(items, item{src: p, arc: arc, inner: inner})
	}
	if !hasArchive {
		return dragSourceData(paths)
	}
	if total > archiveDragMax {
		return transfer.Data{}
	}
	out := make([]string, 0, len(items))
	cacheDirs := map[string]string{}
	for _, it := range items {
		if it.arc == "" {
			out = append(out, it.src)
			continue
		}
		dir, ok := cacheDirs[it.arc]
		if !ok {
			var err error
			dir, err = archiveOpenCacheDir(it.arc)
			if err != nil {
				return transfer.Data{}
			}
			cacheDirs[it.arc] = dir
		}
		written, err := extractArchiveEntry(it.arc, it.inner, dir)
		if err != nil {
			return transfer.Data{}
		}
		out = append(out, written)
	}
	return dragSourceData(out)
}

// canExtractTo reports whether 「解压到…」 has anything to do: an archive
// selection at a real folder, or any current folder inside an archive.
func (a *App) canExtractTo() bool {
	if a.inArchive() {
		return true
	}
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return false
	}
	for _, e := range sel {
		if !entryIsArchive(e) {
			return false
		}
	}
	return true
}

// doExtractTo asks for a destination folder and unpacks the selected
// archives, or the selected entries / current folder inside an archive.
func (a *App) doExtractTo() {
	if !a.canExtractTo() {
		return
	}
	arc, inner, inside := archiveSplit(a.cur().path)
	sel := a.selectedEntries()
	start := a.cur().path
	if inside {
		start = filepath.Dir(arc)
	}
	a.pickExtractFolder(start, func(dst string) {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			a.statusMsg = "解压失败：" + err.Error()
			return
		}
		if inside {
			var inners []string
			if len(sel) == 0 {
				inners = []string{inner}
				if inner == "" {
					// The archive root: unpack every member into dst.
					a.statusMsg = "正在解压…"
					go func() {
						err := extractArchive(arc, dst)
						a.postAssist(func() {
							if err != nil {
								a.statusMsg = "解压失败：" + err.Error()
								return
							}
							a.reload()
							a.statusMsg = "已解压到 " + filepath.Base(dst)
						})
					}()
					return
				}
			} else {
				for _, e := range sel {
					if _, in, ok := archiveSplit(e.Path); ok && in != "" {
						inners = append(inners, in)
					}
				}
			}
			if len(inners) == 0 {
				return
			}
			a.statusMsg = "正在解压…"
			a.extractMembersTo(arc, inners, dst)
			return
		}
		var archives []string
		for _, e := range sel {
			if entryIsArchive(e) {
				archives = append(archives, e.Path)
			}
		}
		if len(archives) == 0 {
			return
		}
		a.statusMsg = fmt.Sprintf("正在解压 %d 个归档…", len(archives))
		go func() {
			var failed []string
			for _, p := range archives {
				if err := extractArchive(p, dst); err != nil {
					failed = append(failed, filepath.Base(p))
				}
			}
			a.postAssist(func() {
				a.reload()
				if len(failed) > 0 {
					a.statusMsg = "解压失败：" + strings.Join(failed, "、")
					return
				}
				a.statusMsg = fmt.Sprintf("已解压 %d 个归档到 %s", len(archives), filepath.Base(dst))
			})
		}()
	})
}

// extractMembersTo unpacks several paths below one archive into dst. Each
// keeps the base name and relative structure the extractor gives it.
func (a *App) extractMembersTo(arc string, inners []string, dst string) {
	go func() {
		var placed, failed []string
		for _, inner := range inners {
			out, err := extractArchiveEntry(arc, inner, dst)
			if err != nil {
				failed = append(failed, filepath.Base(inner))
				continue
			}
			placed = append(placed, out)
		}
		a.postAssist(func() {
			a.reload()
			if len(placed) > 0 && pathsEqual(a.cur().path, dst) {
				a.selectPaths(placed)
			}
			switch {
			case len(failed) > 0:
				a.statusMsg = "解压失败：" + strings.Join(failed, "、")
			case len(placed) > 0:
				a.statusMsg = fmt.Sprintf("已解压 %d 个项目到 %s", len(placed), filepath.Base(dst))
			}
		})
	}()
}

// copyOutArchives extracts archive-internal sources into dstDir in one
// background goroutine and reports the outcome. It is the copy half of
// paste and drop: the source stays compressed, only its bytes land.
func (a *App) copyOutArchives(paths []string, dstDir, verb string) {
	if len(paths) == 0 {
		return
	}
	dstDir = filepath.Clean(dstDir)
	go func() {
		var placed, failed []string
		for _, p := range paths {
			arc, inner, ok := archiveSplit(p)
			if !ok || inner == "" {
				failed = append(failed, filepath.Base(p))
				continue
			}
			out, err := extractArchiveEntry(arc, inner, dstDir)
			if err != nil {
				failed = append(failed, filepath.Base(p))
				continue
			}
			placed = append(placed, out)
		}
		a.postAssist(func() {
			a.reload()
			if len(placed) > 0 && pathsEqual(a.cur().path, dstDir) {
				a.selectPaths(placed)
			}
			switch {
			case len(failed) > 0:
				a.statusMsg = "无法" + verb + "：" + strings.Join(failed, "、")
			case len(placed) > 0:
				a.statusMsg = fmt.Sprintf("已%s %d 个项目到 %s", verb, len(placed), filepath.Base(dstDir))
			}
		})
	}()
}

// selectPaths replaces the selection with paths, as a completed copy does.
func (a *App) selectPaths(paths []string) {
	t := a.cur()
	t.selected = map[string]bool{}
	for _, p := range paths {
		t.selected[p] = true
	}
	if len(paths) > 0 {
		t.anchor = paths[0]
	}
}

// pickExtractFolder runs the native folder chooser with the same
// off-thread watchdog as the assistant's picker.
func (a *App) pickExtractFolder(start string, done func(string)) {
	win := a.win
	dir := assistDialogDir(start)
	a.runAssistPick(func() ([]string, error) {
		return mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "选择解压目标文件夹",
			DefaultPath: dir,
			ButtonLabel: "选择",
			Directory:   true,
		})
	}, func(paths []string, err error) {
		if err != nil {
			a.statusMsg = err.Error()
			return
		}
		if len(paths) == 0 || paths[0] == "" {
			return
		}
		done(paths[0])
	})
}
