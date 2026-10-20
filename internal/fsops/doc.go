// Package fsops holds the file manager's filesystem operations that do
// not depend on the window: copying files and folders, choosing a free
// name for a paste, a new folder or the trash, pasting one item, and the
// trash metadata formats (.trashinfo and Windows $I files). The UI in
// package main calls these and keeps the selection, the status line and
// the undo stack itself.
package fsops
