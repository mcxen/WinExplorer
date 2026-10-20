package remote

import "strings"

// ValidAccountName accepts 1..32 letters, digits, underscores and
// hyphens for the admin username.
func ValidAccountName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// ValidBatchName reports whether a batch name (the names arrays of
// move, copy and remove) addresses one entry of its directory: not empty,
// not "." or "..", and free of separators, so it can neither escape its
// directory nor carry a sub-path.
func ValidBatchName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}
