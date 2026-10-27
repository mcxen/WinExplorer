package main

// assistant_attach.go handles files named for the next assistant message:
// the picker, current-folder selection, pasted paths, and drag-drop.
// Split from assistant.go for maintainability (LAY2).

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *App) assistantPickFiles() {
	win := a.win
	dir := assistDialogDir(a.cur().path)
	a.runAssistPick(func() ([]string, error) {
		return mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "指定要读取的文件",
			DefaultPath: dir,
			ButtonLabel: "添加",
			Message:     "可以多选。助手会读这些文件后再回答。",
			Multiple:    true,
		})
	}, func(paths []string, err error) {
		if err != nil {
			a.statusMsg = err.Error()
			return
		}
		n := a.assist.addFiles(paths)
		if n > 0 {
			a.noteUsedPaths(2, paths...)
			a.statusMsg = "已加入 " + itoa(n) + " 个项目"
		} else if len(paths) > 0 {
			a.statusMsg = "没有新的文件"
		}
	})
}

func (a *App) assistantAttachSelection() {
	var paths []string
	for _, e := range a.selectedEntries() {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	if len(paths) == 0 {
		a.statusMsg = "先在列表里选中文件"
		return
	}
	n := a.assist.addFiles(paths)
	if n == 0 {
		a.statusMsg = "没有新的文件"
		return
	}
	a.noteUsedPaths(2, paths...)
	a.statusMsg = "已加入 " + itoa(n) + " 个项目"
}

func (a *App) attachPastedPaths(text string) {
	if strings.Count(text, "\n") > 20 {
		return
	}
	var paths []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r'
	}) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := os.Stat(strings.Trim(line, "\"'")); err == nil {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return
	}
	n := a.assist.addFiles(paths)
	if n > 0 {
		a.statusMsg = "已加入 " + itoa(n) + " 个项目"
	}
}

// bindAssistDrop makes the transcript and the composer one target. A drop
// names the paths for the next message (addFiles). It does not move or
// copy: the conversation is not a folder. The card keeps AcceptFiles for
// the FileDrop path; registering DropOptions there too would take the
// native drop away from this column and skip dropTookOwnDrag.
func (a *App) bindAssistDrop(el ui.Element) {
	if over, ok := ui.DragOver[dragPayload](el); ok && len(over.paths) > 0 {
		el.Background(colorSelected().Alpha(0.75))
	}
	a.bindNativeDrop(el, func(paths []string) {
		a.attachDroppedPaths(paths)
	})
	if el.FileDragOver() {
		el.Background(colorSelected().Alpha(0.75))
	}
	if p, ok := ui.Drop[dragPayload](el); ok && len(p.paths) > 0 {
		a.attachDroppedPaths(p.paths)
		a.dropTookOwnDrag = true
	}
	if files := el.DroppedFiles(); len(files) > 0 {
		a.attachDroppedPaths(files)
	}
}

// attachDroppedPaths hangs local paths on the assistant. A file URL from
// another application is a path; a remote link is not, and is said so.
func (a *App) attachDroppedPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	local := make([]string, 0, len(paths))
	rejected := false
	for _, p := range paths {
		path, ok := localFilePath(p)
		if !ok {
			rejected = true
			continue
		}
		local = append(local, path)
	}
	n := 0
	if len(local) > 0 {
		n = a.assist.addFiles(local)
	}
	switch {
	case n > 0:
		a.statusMsg = "已加入 " + itoa(n) + " 个项目"
	case len(local) > 0:
		a.statusMsg = "没有新的文件"
	case rejected:
		a.statusMsg = "只能指定本地文件或文件夹"
	}
}

func (u *assistantUI) addFiles(paths []string) int {
	seen := map[string]bool{}
	for _, f := range u.files {
		seen[f.Path] = true
	}
	added := 0
	for _, raw := range paths {
		raw = strings.Trim(strings.TrimSpace(raw), "\"'")
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if seen[abs] || len(u.files) >= assistReadFiles {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		detail := "文件夹"
		if !info.IsDir() {
			detail = formatBytes(info.Size())
		}
		u.files = append(u.files, aiAttach{Path: abs, Name: filepath.Base(abs), Detail: detail})
		seen[abs] = true
		u.litChip(abs)
		added++
	}
	if added > 0 && strings.TrimSpace(u.draft) == "" {
		u.draft = "请阅读这些文件"
	}
	return added
}
