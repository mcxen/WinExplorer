package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSquarify checks the classic properties: the cells tile the rect,
// their areas follow the sizes, and larger items keep larger cells.
func TestSquarify(t *testing.T) {
	items := makeNodes(8, 4, 2, 1)
	cells := squarify(items, ui.Rect{X: 0, Y: 0, W: 100, H: 100})
	if len(cells) != 4 {
		t.Fatalf("squarify gave %d cells, want 4", len(cells))
	}
	var area float32
	for i, c := range cells {
		if c.x < 0 || c.y < 0 || c.x+c.w > 100.01 || c.y+c.h > 100.01 {
			t.Fatalf("cell %d (%q) out of bounds: %v %v %v %v", i, c.node.Name, c.x, c.y, c.w, c.h)
		}
		area += c.w * c.h
		want := float32(items[i].Size) / 15 * 100 * 100
		if diff := c.w*c.h - want; diff < -want*0.05 || diff > want*0.05 {
			t.Fatalf("cell %d (%q) area %.1f, want ~%.1f", i, c.node.Name, c.w*c.h, want)
		}
	}
	if area < 9990 || area > 10010 {
		t.Fatalf("cells cover %.1f of the 10000 rect", area)
	}
	for i := 1; i < len(cells); i++ {
		if cells[i].w*cells[i].h > cells[i-1].w*cells[i-1].h {
			t.Fatalf("cell %d's area exceeds cell %d's", i, i-1)
		}
	}
}

// TestSquarifyDegenerate checks the guards: no items, zero sizes and an
// empty rect give no cells instead of a divide by zero.
func TestSquarifyDegenerate(t *testing.T) {
	if cells := squarify(nil, ui.Rect{W: 100, H: 100}); len(cells) != 0 {
		t.Fatalf("squarify(nil) gave %d cells, want 0", len(cells))
	}
	if cells := squarify(makeNodes(0, 0), ui.Rect{W: 100, H: 100}); len(cells) != 0 {
		t.Fatalf("squarify(zero weights) gave %d cells, want 0", len(cells))
	}
	if cells := squarify(makeNodes(5), ui.Rect{}); len(cells) != 0 {
		t.Fatalf("squarify(empty rect) gave %d cells, want 0", len(cells))
	}
}

// TestUsageCellsMerge checks the share floor: children under 0.4% merge
// into one trailing 其他 cell.
func TestUsageCellsMerge(t *testing.T) {
	children := []*usageNode{{Name: "big", Size: 990}}
	for i := 0; i < 10; i++ {
		children = append(children, &usageNode{Name: fmt.Sprintf("t%d", i), Size: 1})
	}
	cells := usageCells(children, ui.Rect{W: 200, H: 100})
	if len(cells) != 2 {
		t.Fatalf("usageCells gave %d cells, want 2 (big + 其他)", len(cells))
	}
	if cells[0].node.Name != "big" || cells[1].node.Name != "其他" {
		t.Fatalf("cells are %q and %q", cells[0].node.Name, cells[1].node.Name)
	}
	if cells[1].node.Size != 10 {
		t.Fatalf("其他 size %d, want 10", cells[1].node.Size)
	}
}

// TestUsageCellsCap checks the cell cap: a folder of many equal children
// draws at most usageMaxCells cells with the tail in one 其他.
func TestUsageCellsCap(t *testing.T) {
	var children []*usageNode
	for i := 0; i < 500; i++ {
		children = append(children, &usageNode{Name: fmt.Sprintf("f%d", i), Size: 1})
	}
	cells := usageCells(children, ui.Rect{W: 400, H: 300})
	if len(cells) != usageMaxCells {
		t.Fatalf("usageCells gave %d cells, want %d", len(cells), usageMaxCells)
	}
	if cells[0].node.Name != "f0" {
		t.Fatalf("largest child dropped: first cell is %q", cells[0].node.Name)
	}
	if cells[len(cells)-1].node.Name != "其他" {
		t.Fatalf("last cell is %q, want the 其他 tail", cells[len(cells)-1].node.Name)
	}
}

// TestSortedUsage sorts largest first, ties keeping folder order.
func TestSortedUsage(t *testing.T) {
	out := sortedUsage([]*usageNode{
		{Name: "b", Size: 2}, {Name: "a", Size: 5}, {Name: "c", Size: 2},
	})
	if out[0].Name != "a" || out[1].Name != "b" || out[2].Name != "c" {
		t.Fatalf("sortedUsage order: %q %q %q", out[0].Name, out[1].Name, out[2].Name)
	}
}

// TestScanUsage checks the tree totals: sizes bottom-up, file counts,
// hidden entries skipped, symlinks never followed or double counted.
func TestScanUsage(t *testing.T) {
	root := t.TempDir()
	write := func(name string, size int) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", 100)
	write("sub/b.bin", 1000)
	write("sub/deep/c.txt", 10)
	write("empty/x.txt", 0)
	write(".hidden/h.txt", 500)
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "link")); err != nil {
		t.Skip("cannot symlink:", err)
	}
	res := scanUsage(root, false)
	if res.err != "" {
		t.Fatalf("scan error: %s", res.err)
	}
	if res.truncated {
		t.Fatal("scan truncated, want complete")
	}
	if res.tree.Size != 1110 {
		t.Fatalf("root size %d, want 1110", res.tree.Size)
	}
	if res.files != 4 {
		t.Fatalf("files %d, want 4", res.files)
	}
	if res.dirs != 4 {
		t.Fatalf("dirs %d, want 4 (root, sub, deep, empty)", res.dirs)
	}
	sub := usageFind(res.tree, filepath.Join(root, "sub"))
	if sub == nil || sub.Size != 1010 {
		t.Fatalf("sub size %v, want 1010", sub)
	}
	if link := usageFind(res.tree, filepath.Join(root, "link")); link != nil {
		t.Fatalf("symlink followed: %v", link)
	}
	if hidden := usageFind(res.tree, filepath.Join(root, ".hidden")); hidden != nil {
		t.Fatalf("hidden folder scanned: %v", hidden)
	}
	again := scanUsage(root, true)
	if again.tree.Size != 1610 {
		t.Fatalf("root size with hidden %d, want 1610", again.tree.Size)
	}
	if again.dirs != 5 {
		t.Fatalf("dirs with hidden %d, want 5", again.dirs)
	}
}

// TestScanUsageTruncated checks the node cap: a scan over more nodes than
// the cap stops and reports itself truncated.
func TestScanUsageTruncated(t *testing.T) {
	old := usageMaxNodes
	usageMaxNodes = 3
	defer func() { usageMaxNodes = old }()
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		p := filepath.Join(root, fmt.Sprintf("f%d.txt", i))
		if err := os.WriteFile(p, []byte{1, 2}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := scanUsage(root, false)
	if !res.truncated {
		t.Fatal("scan not truncated, want truncated at the node cap")
	}
	if res.files != 3 {
		t.Fatalf("files %d, want the 3 the cap allows", res.files)
	}
}

// TestScanUsageMissing checks an unreadable root: the scan reports the
// error instead of panicking.
func TestScanUsageMissing(t *testing.T) {
	res := scanUsage(filepath.Join(t.TempDir(), "没有这个文件夹"), false)
	if res.err == "" {
		t.Fatal("scan of a missing folder gave no error")
	}
}

// TestUsageFind looks the drill targets up in the tree.
func TestUsageFind(t *testing.T) {
	tree := &usageNode{
		Name: "root", Path: "/root", IsDir: true,
		Children: []*usageNode{
			{Name: "a.txt", Path: "/root/a.txt", Size: 1},
			{Name: "sub", Path: "/root/sub", IsDir: true, Children: []*usageNode{
				{Name: "deep", Path: "/root/sub/deep", IsDir: true},
			}},
		},
	}
	if got := usageFind(tree, "/root/sub/deep"); got == nil || got.Name != "deep" {
		t.Fatalf("usageFind deep = %v, want deep", got)
	}
	if got := usageFind(tree, "/root/a.txt"); got == nil || got.IsDir {
		t.Fatalf("usageFind file = %v, want a.txt", got)
	}
	if got := usageFind(tree, "/root/missing"); got != nil {
		t.Fatalf("usageFind missing = %v, want nil", got)
	}
	if got := usageFind(nil, "/root"); got != nil {
		t.Fatalf("usageFind(nil) = %v, want nil", got)
	}
}

func makeNodes(sizes ...int64) []*usageNode {
	nodes := make([]*usageNode, len(sizes))
	for i, s := range sizes {
		nodes[i] = &usageNode{Name: fmt.Sprintf("n%d", i), Size: s}
	}
	return nodes
}
