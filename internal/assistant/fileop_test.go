package assistant

import "testing"

func TestDiffHunks(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\n"
	after := "a\nb\nC\nd\ne\nf\ng\nh\n"
	got := DiffHunks(DiffLines(before, after), 1)
	want := []DiffLine{{' ', "b"}, {'-', "c"}, {'+', "C"}, {' ', "d"}, {' ', "…"}, {' ', "g"}, {'+', "h"}}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
