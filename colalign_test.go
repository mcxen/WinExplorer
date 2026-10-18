package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// colalign_test.go pins the details view's geometry: the column headers — and
// the width-drag grips at their right edges — sit exactly over the columns of
// data under them.

// TestDetailsHeaderColumnsSitOnTheColumns checks each header column against the
// data cell under it. A cell's text is inset cellPadX, so a header cell's left
// edge must be its data text's left edge less that inset.
func TestDetailsHeaderColumnsSitOnTheColumns(t *testing.T) {
	for _, nameW := range []float32{0, 400} {
		t.Run(fmt.Sprintf("name-column-%.0f", nameW), func(t *testing.T) {
			checkColumns(t, nameW)
		})
	}
}

// checkColumns asserts the header columns sit over the data with the name
// column at nameW (0 keeps it flexible, as it starts).
func checkColumns(t *testing.T, nameW float32) {
	t.Helper()
	const cellPadX float32 = 8
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, tt, _ := keynavWindow(t, 1)
	a.mode = ViewDetails
	a.colWidths.name = nameW
	a.navigate(dir)
	tt.Frame()
	entry := a.visibleEntries()[0]

	// The header columns, in order.
	var headers []ui.Rect
	for _, title := range []string{"名称", "修改日期", "类型", "大小"} {
		r, ok := tt.Find(title)
		if !ok || r.W == 0 {
			t.Fatalf("the header %q is not in the window: %v", title, tt.Texts())
		}
		headers = append(headers, r)
	}
	// They tile the row: no gap between one column and the next, which is what
	// per-cell insets used to put there.
	for i := 1; i < len(headers); i++ {
		if gap := headers[i].X - (headers[i-1].X + headers[i-1].W); gap < -1 || gap > 1 {
			t.Fatalf("the header columns %d and %d leave a %.1f DIP gap: %v", i-1, i, gap, headers)
		}
	}
	// Each column's left edge is the data's, for the columns the rows draw
	// left-aligned (the name column starts at its icon, and 大小 is aligned to
	// the right edge instead).
	for i, text := range map[int]string{
		1: formatModTime(entry.ModTime),
		2: typeLabel(entry),
	} {
		cell, ok := tt.Find(text)
		if !ok {
			t.Fatalf("the cell %q is not in the window", text)
		}
		if got := headers[i].X; got < cell.X-cellPadX-1 || got > cell.X-cellPadX+1 {
			t.Fatalf("the %q column starts at %.1f, but its data starts at %.1f (cell %q)",
				[]string{"名称", "修改日期", "类型", "大小"}[i], got, cell.X-cellPadX, text)
		}
	}
	// 大小 is right-aligned like its data: the column covers its data, and no
	// more than the cell's own right inset beyond it.
	size, ok := tt.Find(sizeColumnText(entry))
	if !ok {
		t.Fatal("the size cell is not in the window")
	}
	last := headers[len(headers)-1]
	if got, want := last.X+last.W, size.X+size.W; got < want-1 || got > want+cellPadX {
		t.Fatalf("the 大小 column ends at %.1f, but its data ends at %.1f (%q)", got, want, sizeColumnText(entry))
	}
}
