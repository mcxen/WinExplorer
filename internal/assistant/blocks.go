package assistant

import "strings"

// CutBlock removes the first delimited block and returns the surrounding prose
// in its original order.
func CutBlock(text, open, close string) (block, rest string, ok bool) {
	i := strings.Index(text, open)
	if i < 0 {
		return "", "", false
	}
	from := i + len(open)
	j := strings.Index(text[from:], close)
	if j < 0 {
		return "", "", false
	}
	block = text[from : from+j]
	rest = strings.TrimSpace(text[:i] + text[from+j+len(close):])
	return block, rest, true
}
