package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestPathInputHelpers covers the address bar's text rules that the
// window tests do not reach on every platform.
func TestPathInputHelpers(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := normalizePathInput(` '~' `); got != home {
		t.Fatalf("~ in quotes: %q", got)
	}
	t.Setenv("WE_PCT", "v")
	if got := expandPercentEnv("a%WE_PCT%b%NOPE_X%c%"); got != "avb%NOPE_X%c%" {
		t.Fatalf("expandPercentEnv: %q", got)
	}
	if got := matchNames([]string{"Zeta", "abc", "Xab", "ABd"}, "ab", 10); !slices.Equal(got, []string{"abc", "ABd", "Xab"}) {
		t.Fatalf("matchNames: %v", got)
	}
	if p, partial, ok := splitForCompletion("/x/y/ab"); !ok || p != "/x/y/" || partial != "ab" {
		t.Fatalf("split: %q %q %v", p, partial, ok)
	}
	if got := absPathInput("sub", filepath.FromSlash("/base")); got != filepath.Join(filepath.FromSlash("/base"), "sub") {
		t.Fatalf("relative: %q", got)
	}
	// The last crumb always shows; leading ones fold until the rest fit.
	if got := crumbsThatFit([]float32{50, 50, 50, 50}, 1000); got != 0 {
		t.Fatalf("roomy: %d", got)
	}
	if got := crumbsThatFit([]float32{50, 50, 50, 50}, 160); got != 2 {
		t.Fatalf("tight: %d", got)
	}
	if got := crumbsThatFit([]float32{50, 50, 500}, 100); got != 2 {
		t.Fatalf("too long: %d", got)
	}
}
