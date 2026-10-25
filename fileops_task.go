package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sunvale/internal/fsops"
	"sunvale/internal/tasks"
)

// fileops_task.go runs copy, move and delete-to-trash as tasks: off the UI
// thread, behind the per-disk slots, pausable and cancelable between files
// and inside a file's bytes. A name already taken in the target is asked
// inline in the task card for the user's own operations (替换 / 跳过 /
// 保留两者, optionally for the rest); AI and scheduled runs keep both.
// What landed goes on the undo stack when the run ends, canceled or not.

type fileJob struct {
	Kind   tasks.Kind // copy, move, delete
	Source tasks.Source
	Srcs   []string
	DstDir string // copy/move target
}

// fileJobResult is what one run did.
type fileJobResult struct {
	Placed  []string // new paths, for the selection
	Failed  []string // names
	Undo    undoOp
	Trashed int
	Verb    string
	Logs    [][2]string // trash restore keys, written to the store on the UI thread
	Err     error
}

func (j fileJob) verb() string {
	switch j.Kind {
	case tasks.KindMove:
		return "移动"
	case tasks.KindDelete:
		return "删除"
	}
	return "复制"
}

func (j fileJob) spec() tasks.Spec {
	title := j.verb() + " "
	if len(j.Srcs) == 1 {
		title += filepath.Base(j.Srcs[0])
	} else {
		title += fmt.Sprintf("%d 个项目", len(j.Srcs))
	}
	path, vol := j.DstDir, j.DstDir
	if j.Kind == tasks.KindDelete && len(j.Srcs) > 0 {
		path, vol = filepath.Dir(j.Srcs[0]), j.Srcs[0]
	}
	if j.DstDir != "" {
		title += " → " + filepath.Base(j.DstDir)
	}
	return tasks.Spec{Kind: j.Kind, Title: title, Source: j.Source, Path: path, IO: "disk", Volume: archiveVolume(vol)}
}

// startFileJob runs j as a task; done runs on the UI thread with the
// result (again after each 重试).
func (a *App) startFileJob(j fileJob, done func(fileJobResult)) string {
	return runTask(j.spec(), func(r *tasks.Reporter) error {
		res := runFileJob(r, j)
		res.Verb = j.verb()
		a.postAssist(func() { done(res) })
		return res.Err
	})
}

func runFileJob(r *tasks.Reporter, j fileJob) fileJobResult {
	var res fileJobResult
	if j.Kind == tasks.KindDelete {
		return runTrashJob(r, j)
	}
	// Totals first, so the bar has a scale: bytes for what will be copied.
	var total int64
	files := 0
	for _, src := range j.Srcs {
		b, n := fsops.TreeSize(src)
		total += b
		files += n
	}
	r.SetTotal(tasks.UnitBytes, total, files)
	move := j.Kind == tasks.KindMove
	sticky := ""
	for i, src := range j.Srcs {
		if err := r.Wait(); err != nil {
			res.Err = err
			break
		}
		src = filepath.Clean(src)
		name := filepath.Base(src)
		info, err := os.Lstat(src)
		if err != nil {
			res.Failed = append(res.Failed, name)
			r.Fail(name, err)
			continue
		}
		if move && filepath.Dir(src) == filepath.Clean(j.DstDir) {
			res.Placed = append(res.Placed, src) // 原地移动是空操作
			b, _ := fsops.TreeSize(src)
			r.Add(b)
			continue
		}
		dst := filepath.Join(j.DstDir, name)
		var replaced *undoMove
		if _, err := os.Lstat(dst); err == nil {
			choice := tasks.ChoiceKeep
			if filepath.Dir(src) != filepath.Clean(j.DstDir) && j.Source == tasks.SourceUser {
				choice = sticky
				if choice == "" {
					ans, err := r.Ask(tasks.Ask{Item: name, Detail: "目标已存在",
						Options: []string{tasks.ChoiceReplace, tasks.ChoiceSkip, tasks.ChoiceKeep}, Rest: len(j.Srcs) - i - 1})
					if err != nil {
						res.Err = err
						break
					}
					choice = ans.Choice
					if ans.All {
						sticky = choice
					}
				}
			}
			switch choice {
			case tasks.ChoiceSkip:
				b, _ := fsops.TreeSize(src)
				r.Add(b)
				continue
			case tasks.ChoiceReplace:
				// The old one goes to the bin, not away: 撤销 brings it back.
				tdst, _, err := moveToTrash(dst)
				if err != nil {
					res.Failed = append(res.Failed, name)
					r.Fail(name, err)
					continue
				}
				if tdst != "" {
					replaced = &undoMove{From: dst, To: tdst}
				}
			default:
				n, err := uniqueFreeName(j.DstDir, name, info.IsDir())
				if err != nil {
					res.Failed = append(res.Failed, name)
					r.Fail(name, err)
					continue
				}
				dst = filepath.Join(j.DstDir, n)
			}
		}
		err = placeOne(r, src, dst, info, move)
		if errors.Is(err, tasks.ErrCanceled) || r.Canceled() {
			if !move {
				_ = os.RemoveAll(dst)
			}
			res.Err = tasks.ErrCanceled
			if replaced != nil {
				res.Undo.Moved = append(res.Undo.Moved, *replaced)
			}
			break
		}
		if err != nil {
			res.Failed = append(res.Failed, name)
			r.Fail(name, err)
			continue
		}
		if replaced != nil {
			res.Undo.Moved = append(res.Undo.Moved, *replaced)
		}
		res.Placed = append(res.Placed, dst)
		if move {
			res.Undo.Moved = append(res.Undo.Moved, undoMove{From: src, To: dst})
		} else {
			res.Undo.Created = append(res.Undo.Created, dst)
		}
	}
	n := len(res.Undo.Created) + len(res.Undo.Moved)
	desc := j.verb()
	if n > 1 {
		desc = fmt.Sprintf("%s %d 个项目", desc, n)
	}
	res.Undo.Desc = desc
	return res
}

// placeOne copies (or moves) src to dst with byte progress; a move tries
// a rename first and falls back to copy-then-remove across devices.
func placeOne(r *tasks.Reporter, src, dst string, info os.FileInfo, move bool) error {
	if move {
		if err := os.Rename(src, dst); err == nil {
			b, _ := fsops.TreeSize(dst)
			r.Add(b)
			return nil
		}
	}
	h := &fsops.Hooks{
		Item: func(rel string, size int64) { r.Item(rel, size) },
		Done: r.ItemDone,
		Wrap: func(rd io.Reader) io.Reader { return waitReader{r.Reader(rd), r} },
		Wait: r.Wait,
	}
	var err error
	if info.IsDir() {
		err = fsops.CopyDirH(src, dst, h)
	} else {
		err = fsops.CopyFileH(src, dst, info, h)
	}
	if err != nil {
		if move {
			_ = os.RemoveAll(dst)
		}
		return err
	}
	if move {
		return os.RemoveAll(src)
	}
	return nil
}

// waitReader honours pause and cancel inside one large file.
type waitReader struct {
	rd io.Reader
	r  *tasks.Reporter
}

func (w waitReader) Read(b []byte) (int, error) {
	if err := w.r.Wait(); err != nil {
		return 0, err
	}
	if len(b) > 1<<20 {
		b = b[:1<<20]
	}
	return w.rd.Read(b)
}

func runTrashJob(r *tasks.Reporter, j fileJob) fileJobResult {
	var res fileJobResult
	r.SetTotal(tasks.UnitItems, int64(len(j.Srcs)), len(j.Srcs))
	for _, p := range j.Srcs {
		if err := r.Wait(); err != nil {
			res.Err = err
			break
		}
		name := filepath.Base(p)
		r.Item(name, 0)
		if p == "" || p == pathThisPC || p == "/" {
			res.Failed = append(res.Failed, name)
			r.Fail(name, errors.New("无法删除"))
			continue
		}
		dst, key, err := moveToTrash(p)
		if err != nil {
			res.Failed = append(res.Failed, name)
			r.Fail(name, err)
			continue
		}
		r.Add(1)
		r.ItemDone()
		res.Trashed++
		if key != "" {
			res.Logs = append(res.Logs, [2]string{p, key})
		}
		if dst != "" {
			res.Undo.Moved = append(res.Undo.Moved, undoMove{From: p, To: dst})
		}
	}
	res.Undo.Desc = "删除"
	if n := len(res.Undo.Moved); n > 1 {
		res.Undo.Desc = fmt.Sprintf("删除 %d 个项目", n)
	}
	return res
}

func joinNames(names []string) string { return strings.Join(names, "、") }
