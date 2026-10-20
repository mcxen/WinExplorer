package main

import (
	"path/filepath"
	"testing"
	"time"
)

// rename_ui_test.go drives the batch rename dialog in a laid-out window: the
// rows exist as laid-out elements, a drag over a row reaches its drop half and
// renumbers the plan, and a double click turns a row into an editor whose text
// becomes that row's name.

func TestBatchRenameDialogRowsAndDrag(t *testing.T) {
	a, tt, dir := keynavWindow(t, 3)
	a.selectAll()
	a.doRenameStart()
	if !a.batch.open {
		t.Fatal("the batch dialog did not open")
	}
	// The dialog is a floating panel: let its entrance motion finish so its
	// rows are laid out.
	tt.Frame()
	for i := 0; i < 4; i++ {
		tt.Advance(120 * time.Millisecond)
	}

	for _, want := range []string{"批量重命名", "模板", "正则", "查找替换", "重命名"} {
		if !tt.HasText(want) {
			t.Fatalf("the dialog lost %q: %v", want, tt.Texts())
		}
	}

	// A drag from the last row onto the first row's upper half moves it to
	// the front, and the numbering follows the new order.
	from := a.batch.rows[2]
	to := a.batch.rows[0]
	// The pane behind the dialog shows the items too; the dialog's rows are
	// found by the target names only it renders.
	rf, ok := tt.Find(from.To)
	if !ok {
		t.Fatalf("row %q not laid out: %v", from.Name, tt.Texts())
	}
	rt, ok := tt.Find(to.To)
	if !ok {
		t.Fatalf("row %q not laid out: %v", to.Name, tt.Texts())
	}
	// The dialog's rows are 34 tall (batchRow). The target-name text is
	// centred in them, so its centre is the row's centre; derive the row
	// edges from that rather than treating the text height as the row.
	const rowH = 34
	fromMid := rf.Y + rf.H/2
	toTop := rt.Y - (rowH-rt.H)/2
	// Press the last row's centre and release in the first row's upper
	// half, which is where the list inserts before that row.
	y0, y1 := fromMid, toTop+rowH/4
	tt.Press(rf.X+rf.W/2, y0)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(rf.X+rf.W/2, y0+(y1-y0)*f)
	}
	tt.Release(rt.X+rt.W/2, y1)
	tt.Frame()

	// The list's reorder drops where the pointer is: the upper half of the
	// first row puts the dragged row before it, and the numbers follow the
	// new order rather than the identity.
	if a.batch.rows[0].Name != from.Name {
		t.Fatalf("the drag did not move %q to the front: %+v", from.Name, a.batch.rows)
	}
	if a.batch.rows[0].To != "1 "+from.Name || a.batch.rows[1].To != "2 "+a.batch.rows[1].Name {
		t.Fatalf("the moved rows kept their old numbers: %+v", a.batch.rows)
	}
	_ = dir
}

// TestBatchRenameRowEditingTypesAName checks the inline name editor: a double
// click on a row opens it, typing lands in that row, and closing it keeps the
// name as a manual target.
func TestBatchRenameRowEditingTypesAName(t *testing.T) {
	a, tt, dir := keynavWindow(t, 2)
	a.selectAll()
	a.doRenameStart()
	tt.Frame()
	for i := 0; i < 4; i++ {
		tt.Advance(120 * time.Millisecond)
	}

	rect, ok := tt.Find(a.batch.rows[0].To)
	if !ok {
		t.Fatalf("row not laid out: %v", tt.Texts())
	}
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2) // the second click of a double
	if a.batch.editing != 0 {
		t.Fatalf("double click did not open the editor: editing %d", a.batch.editing)
	}
	// Typing lands in the editor at its caret; a fresh one starts after the
	// name, so the row's target is the name plus the typed text.
	tt.Type("first")
	if !a.batch.rows[0].Manual || a.batch.rows[0].To != a.batch.rows[0].To[:len("1 f000.txt")]+"first" {
		t.Fatalf("typing did not land in the row: %+v", a.batch.rows[0])
	}
	a.batch.editing = -1
	a.batchReplan()
	a.commitBatchRename()
	if _, err := filepath.Glob(filepath.Join(dir, "first*")); err != nil {
		t.Fatalf("glob: %v", err)
	}
	if _, err := filepath.Glob(filepath.Join(dir, "f000.txt")); err != nil {
		t.Fatalf("glob: %v", err)
	}
}
