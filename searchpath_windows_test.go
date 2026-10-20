//go:build windows

package main

// searchpath_windows_test.go pins Windows path comparisons: a search root
// and a hit may spell the same folders in different case.

import "testing"

func TestSearchPathWindowsCaseInsensitive(t *testing.T) {
	home := `C:\Users\Alice`
	dir := `c:\users\alice\Documents`
	if got := searchPathText(searchPathSegments(dir, home, 5)); got != `主文件夹 › Documents` {
		t.Fatalf("segments = %q, want the home breadcrumb", got)
	}
	e := Entry{Dir: dir}
	if got := searchLocationText(e, `C:\USERS\ALICE`, home); got != "Documents" {
		t.Fatalf("location = %q, want a path relative to the root", got)
	}
}
