package forms

import (
	"strings"
)

// ListItem is one row in a KindList field. Options encode items as
// "name|size|thumb" (size and thumb optional). The live form value is a
// comma-separated list of selected Names.
type ListItem struct {
	Name  string
	Size  string
	Thumb string
}

// ParseListItem splits one Options entry. Bare names (no "|") are fine.
func ParseListItem(s string) ListItem {
	s = strings.TrimSpace(s)
	if s == "" {
		return ListItem{}
	}
	parts := strings.SplitN(s, "|", 3)
	it := ListItem{Name: strings.TrimSpace(parts[0])}
	if len(parts) > 1 {
		it.Size = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		it.Thumb = strings.TrimSpace(parts[2])
	}
	return it
}

// FormatListItem builds an Options entry from a ListItem.
func FormatListItem(it ListItem) string {
	name := strings.TrimSpace(it.Name)
	size := strings.TrimSpace(it.Size)
	thumb := strings.TrimSpace(it.Thumb)
	switch {
	case thumb != "":
		return name + "|" + size + "|" + thumb
	case size != "":
		return name + "|" + size
	default:
		return name
	}
}

// ListItemNames returns the Name of each option (for selection matching).
func ListItemNames(opts []string) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		it := ParseListItem(o)
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out
}
