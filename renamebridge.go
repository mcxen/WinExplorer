package main

import "sunvale/internal/rename"

// renameRowsFromEntries makes a plan out of the selection, in view order —
// the order the numbers run in, and the order a drag changes. The birth
// time is the engine's, read here because the listing's Entry does not
// carry one.
func renameRowsFromEntries(entries []Entry) []rename.Row {
	items := make([]rename.Source, len(entries))
	for i, e := range entries {
		items[i] = rename.Source{
			Name: e.Name, Path: e.Path, IsDir: e.IsDir, Ext: e.Ext, ModTime: e.ModTime,
		}
		if !e.IsDir && e.Path != "" {
			if bt, err := rename.BirthTime(e.Path); err == nil {
				items[i].Created = bt
			}
		}
	}
	return rename.RowsFrom(items)
}
