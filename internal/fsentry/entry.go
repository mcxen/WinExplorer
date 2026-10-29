// Package fsentry holds the file-list entry shared by the application and
// reusable filesystem helpers, without depending on the window UI.
package fsentry

import "time"

// Entry is one file-system row as the application lists it.
type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Ext     string // lower, no dot; empty for dirs

	// Dir is the parent folder's path, shown in the search result's 位置
	// column; empty for regular listings.
	Dir string
	// VolFree/VolTotal carry a drive's capacity for the 此电脑 page's
	// usage bars; zero otherwise.
	VolFree, VolTotal int64
	// Link marks a symbolic link (drawn with the shortcut badge); NoSize a
	// file whose length is not its content — a device, a pipe, a /proc or
	// /sys pseudo-file — whose 大小 cell stays blank.
	Link, NoSize bool
}
