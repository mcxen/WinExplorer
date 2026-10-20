// Package searchquery is the search box's query language, Everything's
// semantics on a filename-only engine:
//
//	a whole-word modifier-less substring,   alpha  →  *alpha*
//	case:alpha                             →  case-sensitive *alpha*
//	file:alpha / folder:alpha              →  hits of that kind only
//	ext:txt alpha                          →  .txt files matching alpha
//	path:docs alpha                        →  hit whose path contains docs
//	"an exact phrase"                      →  the phrase verbatim
//	terms separated by spaces              →  AND, every term must match
//
// Everything's other modifiers (regex:, dupe:, count:, size:, dm:, ...)
// stay out: the engine is a bounded walk, and a query the engine cannot
// answer should read as what it is — plain search terms — instead of
// silently meaning something else. The one exception is wholefile:, which
// would need content reads on a filename walk.
//
// case: is position-dependent as in Everything: it toggles case
// sensitivity from the term it prefixes to the end of the query —
// "alpha case:BETA" matches "alpha" insensitively and "BETA" strictly.
package searchquery

import "strings"

// Query is one parsed query: the conjunction every candidate name (or
// path, for path:) must satisfy. The terms are already normalized:
// lowercase-folded unless case-sensitive, phrases verbatim.
type Query struct {
	terms     []term
	sensitive bool
	filtering filterKind // file: / folder:
	exts      []string   // ext:, lowercase, no dots; nil without ext:
}

// term is one conjunct of a query.
type term struct {
	text string // the phrase to find
	kind termKind
	// caseSensitive freezes case:'s state at this term's position: terms
	// before the first case: stay insensitive, terms after it strict,
	// exactly as Everything's positional modifier does.
	caseSensitive bool
}

type termKind int

const (
	termName termKind = iota // match against the file name
	termPath                 // match against the whole path
)

// filterKind is what file:/folder: asked the results to keep.
type filterKind int

const (
	filterAny filterKind = iota
	filterFile
	filterFolder
)

// Parse parses the raw query. A query with no usable term matches nothing;
// the callers treat an empty string as "no search" before they get here.
func Parse(raw string) Query {
	q := Query{}
	fields := splitTerms(raw)
	filtering := filterAny
	var exts []string
	var terms []term
	for _, f := range fields {
		if f == "" {
			continue
		}
		// case: toggles from here on.
		if strings.EqualFold(f, "case:") {
			q.sensitive = !q.sensitive
			continue
		}
		if prefix, rest, ok := cutModifier(f); ok {
			switch prefix {
			case "file", "folder":
				// file:/folder: filter the kind; their rest, when there is
				// one, is also a name term — Everything's "folder:rep"
				// means folders whose name contains rep.
				if prefix == "file" {
					filtering = filterFile
				} else {
					filtering = filterFolder
				}
				if rest != "" {
					terms = append(terms, term{text: rest, caseSensitive: q.sensitive})
				}
				continue
			case "path":
				// path: alone is not a term; path:docs is.
				if rest != "" {
					terms = append(terms, term{text: rest, kind: termPath})
				}
				continue
			case "case":
				// case: toggles from here on; its rest, if any, is the
				// next term (case:BETA is case: followed by BETA).
				q.sensitive = !q.sensitive
				if rest != "" {
					terms = append(terms, term{text: rest, caseSensitive: q.sensitive})
				}
				continue
			case "ext":
				for _, e := range strings.Split(rest, ";") {
					e = strings.ToLower(strings.Trim(strings.TrimSpace(e), "."))
					if e != "" {
						exts = append(exts, e)
					}
				}
				continue
			}
		}
		terms = append(terms, term{text: f, caseSensitive: q.sensitive})
	}
	q.terms = terms
	q.filtering = filtering
	q.exts = exts
	return q
}

// cutModifier splits "prefix:rest" when prefix is a known modifier word
// and rest is either empty or non-empty text. ok is false for anything
// that is not exactly "word:" or "word:text" with word in the modifier
// set — "d:\path" and "http://" must survive as ordinary terms.
func cutModifier(field string) (prefix, rest string, ok bool) {
	for _, m := range []string{"file", "folder", "path", "ext", "case"} {
		if len(field) == len(m)+1 && strings.EqualFold(field[:len(m)], m) && field[len(m)] == ':' {
			return m, "", true
		}
		if len(field) > len(m)+1 && strings.EqualFold(field[:len(m)], m) && field[len(m)] == ':' {
			return m, field[len(m)+1:], true
		}
	}
	return "", "", false
}

// splitTerms splits the query on spaces outside quotes, keeping each
// quoted phrase as one verbatim term (its quotes dropped). Everything
// treats "a b" as the single phrase `a b`.
func splitTerms(q string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' || r == '\t':
			if inQuote {
				cur.WriteRune(r)
			} else {
				flush()
			}
		default:
			cur.WriteRune(r)
		}
	}
	// A phrase left open by a missing quote ends at the end of the query.
	flush()
	return out
}

// Matches reports whether name and path satisfy the query: every term
// present, and the file/folder and ext filters honored. isDir tells the
// kind; ext is the entry's lowercased extension without a dot. Hidden
// entries are filtered by the walk, not here.
func (q Query) Matches(name, path, ext string, isDir bool) bool {
	switch q.filtering {
	case filterFile:
		if isDir {
			return false
		}
	case filterFolder:
		if !isDir {
			return false
		}
	}
	if len(q.exts) > 0 {
		if isDir {
			return false
		}
		found := false
		for _, e := range q.exts {
			if e == ext {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, t := range q.terms {
		hay := name
		needle := t.text
		if t.kind == termPath {
			hay = path
		}
		if !t.caseSensitive {
			hay = strings.ToLower(hay)
			needle = strings.ToLower(needle)
		}
		if !strings.Contains(hay, needle) {
			return false
		}
	}
	return true
}

// Span is one run of a name to paint: the text, and whether it is a hit to
// highlight. The runs concatenate back to the full name.
type Span struct {
	Text string
	Hit  bool
}

// Highlight splits name into runs around the query's matches, Everything's
// yellow-mark style: every term's occurrence gets a background. Overlaps
// merge into one run. A case-sensitive query highlights exact matches
// only; a phrase term highlights the whole phrase.
func (q Query) Highlight(name string) []Span {
	if len(q.terms) == 0 {
		return []Span{{Text: name}}
	}
	lower := strings.ToLower(name)
	marks := make([]bool, len(lower))
	any := false
	for _, t := range q.terms {
		needle := t.text
		if t.kind == termPath {
			continue // path terms do not highlight the name
		}
		if !t.caseSensitive {
			needle = strings.ToLower(needle)
		}
		if needle == "" {
			continue
		}
		// Byte-level marking is safe: both needle and hay are valid UTF-8,
		// so a byte offset of one is always a rune boundary of the other.
		// A case-sensitive term scans the name as typed, the insensitive
		// ones its lowercase fold.
		hay := lower
		if t.caseSensitive {
			hay = name
		}
		from := 0
		for {
			i := strings.Index(hay[from:], needle)
			if i < 0 {
				break
			}
			s := from + i
			for b := 0; b < len(needle); b++ {
				marks[s+b] = true
			}
			any = true
			from = s + len(needle)
		}
	}
	if !any {
		return []Span{{Text: name}}
	}
	var out []Span
	var b strings.Builder
	prev := false
	started := false
	flush := func(hit bool) {
		if b.Len() > 0 {
			out = append(out, Span{Text: b.String(), Hit: hit})
			b.Reset()
		}
	}
	for i, m := range marks {
		if started && m != prev {
			flush(prev)
		}
		b.WriteByte(name[i])
		prev = m
		started = true
	}
	flush(prev)
	return out
}
