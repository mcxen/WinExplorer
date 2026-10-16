package main

import (
	"os"
	"path/filepath"
)

// undo.go is the Ctrl+Z stack, Explorer's 撤销: every operation records
// what it did, and undo reverses the most recent one. Two shapes cover all
// of them: files created (copy, new folder, new text, compress, extract)
// and files moved (paste-cut, drag move, rename, delete to trash,
// restore).

type undoMove struct {
	From, To string
}

type undoOp struct {
	desc    string
	created []string   // undo removes these
	moved   []undoMove // undo moves each back From←To
}

// pushUndo records an operation, newest last, capped at maxUndoKeep.
func (a *App) pushUndo(op undoOp) {
	if len(op.created) == 0 && len(op.moved) == 0 {
		return
	}
	a.undo = append(a.undo, op)
	if len(a.undo) > maxUndoKeep {
		a.undo = a.undo[len(a.undo)-maxUndoKeep:]
	}
}

// canUndo reports whether Ctrl+Z has anything to undo.
func (a *App) canUndo() bool { return len(a.undo) > 0 }

// doUndo reverses the most recent operation, as Explorer's Ctrl+Z does.
// A moved file that has since gone missing, or a target that exists
// again, is skipped with a note rather than clobbering anything.
func (a *App) doUndo() {
	if len(a.undo) == 0 {
		return
	}
	op := a.undo[len(a.undo)-1]
	a.undo = a.undo[:len(a.undo)-1]

	var failed []string
	// Moves go back first, newest operation's moves in reverse order.
	for i := len(op.moved) - 1; i >= 0; i-- {
		m := op.moved[i]
		if _, err := os.Lstat(m.To); err != nil {
			failed = append(failed, filepath.Base(m.From))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(m.From), 0o755); err != nil {
			failed = append(failed, filepath.Base(m.From))
			continue
		}
		if _, err := os.Lstat(m.From); err == nil {
			failed = append(failed, filepath.Base(m.From)) // something took its place
			continue
		}
		if err := os.Rename(m.To, m.From); err != nil {
			failed = append(failed, filepath.Base(m.From))
		}
	}
	// Then created files are removed.
	for _, p := range op.created {
		if err := os.RemoveAll(p); err != nil {
			failed = append(failed, filepath.Base(p))
		}
	}

	a.reload()
	a.clearSelection()
	if len(failed) > 0 {
		a.statusMsg = "部分项目已撤销；无法撤销：" + joinComma(failed)
	} else {
		a.statusMsg = "已撤销：" + op.desc
	}
}

func joinComma(names []string) string {
	s := ""
	for i, n := range names {
		if i > 0 {
			s += "、"
		}
		s += n
	}
	return s
}
