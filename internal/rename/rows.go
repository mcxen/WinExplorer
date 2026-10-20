package rename

import "time"

// Source is one selected item a plan starts from.
type Source struct {
	Name    string
	Path    string
	IsDir   bool
	Ext     string
	ModTime time.Time
	Created time.Time
}

// RowsFrom builds a plan from the selection, in view order — the order the
// numbers run in, and the order a drag changes.
func RowsFrom(items []Source) []Row {
	rows := make([]Row, len(items))
	for i, e := range items {
		rows[i] = Row{
			Name:    e.Name,
			Path:    e.Path,
			IsDir:   e.IsDir,
			Ext:     e.Ext,
			ModTime: e.ModTime,
			Created: e.Created,
		}
	}
	return rows
}
