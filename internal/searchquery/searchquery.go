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

import (
	"path/filepath"
	"strings"
	"time"
)

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
					terms = append(terms, term{text: rest, kind: termPath, caseSensitive: q.sensitive})
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

// MatchKind says why a hit matched: its name, or only its path.
type MatchKind int

const (
	MatchNone MatchKind = iota
	MatchName
	MatchPath
)

// Label is the short Chinese name a result row shows for the kind: the
// reason the hit is in the list. An unknown kind, and MatchNone in
// particular, has no label.
func (k MatchKind) Label() string {
	switch k {
	case MatchName:
		return "文件名"
	case MatchPath:
		return "路径"
	}
	return ""
}

// Reason is one contribution to a hit's relevance, in the words the preview
// can show beside the score.
type Reason struct {
	Text   string
	Weight int
}

// Score ranks a hit: higher is better. now is passed in so tests are stable,
// and home lets common folders be favored without the package reading the
// machine's state. The caller has already checked Matches; Score describes
// why a matching hit deserves its place.
//
// Priority, highest first: the name match (exact stem, then prefix, then
// substring), path-only hits below all of them, then recency, depth, a
// cache penalty and a home bonus as tiebreakers.
func (q Query) Score(name, path string, isDir bool, mod, now time.Time, home string) (int, MatchKind, []Reason) {
	score := 0
	kind := MatchNone
	var reasons []Reason
	nameBest := 0
	nameTerm := ""
	for _, t := range q.terms {
		if t.kind == termPath {
			continue
		}
		hay, needle := name, t.text
		if !t.caseSensitive {
			hay, needle = strings.ToLower(hay), strings.ToLower(needle)
		}
		if needle == "" || !strings.Contains(hay, needle) {
			continue
		}
		kind = MatchName
		weight := 25
		stem := strings.TrimSuffix(hay, strings.ToLower(filepath.Ext(hay)))
		switch {
		case stem == needle:
			weight = 60
		case strings.HasPrefix(hay, needle):
			weight = 40
		}
		if weight > nameBest {
			nameBest, nameTerm = weight, needle
		}
	}
	if kind == MatchName {
		switch nameBest {
		case 60:
			reasons = append(reasons, Reason{Text: "文件名完全匹配", Weight: nameBest})
		case 40:
			reasons = append(reasons, Reason{Text: "文件名以“" + nameTerm + "”开头", Weight: nameBest})
		default:
			reasons = append(reasons, Reason{Text: "文件名包含“" + nameTerm + "”", Weight: nameBest})
		}
		// The name match dominates: score it in hundreds while its public
		// weight stays the small number the reason label reads.
		score += nameBest * 100
	}
	for _, t := range q.terms {
		if t.kind != termPath {
			continue
		}
		hay, needle := path, t.text
		if !t.caseSensitive {
			hay, needle = strings.ToLower(hay), strings.ToLower(needle)
		}
		if needle != "" && strings.Contains(hay, needle) {
			if kind == MatchNone {
				kind = MatchPath
				score += 5
				reasons = append(reasons, Reason{Text: "路径命中", Weight: 5})
			}
			break
		}
	}
	// Two or more name terms are a stronger match than one, without
	// overtaking the name/path distinction above.
	extraName := 0
	for _, t := range q.terms {
		if t.kind == termPath {
			continue
		}
		hay, needle := name, t.text
		if !t.caseSensitive {
			hay, needle = strings.ToLower(hay), strings.ToLower(needle)
		}
		if needle != "" && strings.Contains(hay, needle) {
			extraName++
		}
	}
	if extraName > 1 {
		weight := min(extraName-1, 3) * 2
		score += weight
		reasons = append(reasons, Reason{Text: "多个查询词都在文件名中", Weight: weight})
	}
	if age := now.Sub(mod); !mod.IsZero() {
		switch {
		case age >= 0 && age <= 7*24*time.Hour:
			score += 8
			reasons = append(reasons, Reason{Text: "最近 7 天内修改", Weight: 8})
		case age > 7*24*time.Hour && age <= 30*24*time.Hour:
			score += 4
			reasons = append(reasons, Reason{Text: "最近 30 天内修改", Weight: 4})
		}
	}
	if penalizedPath(path) {
		score -= 20
		reasons = append(reasons, Reason{Text: "位于缓存或生成目录，降低排序", Weight: -20})
	}
	// The hit's own folder counts, but the search root does not. Without
	// the root, a temp home's four leading folders would outweigh every
	// name score; capping at ten keeps the penalty a tiebreaker.
	if depth := pathDepth(filepath.Dir(path)); depth > 1 {
		penalty := min(depth-1, 10)
		score -= penalty
		if penalty > 0 {
			reasons = append(reasons, Reason{Text: "路径较深", Weight: -penalty})
		}
	}
	if home != "" && pathWithin(path, home) {
		score += 5
		reasons = append(reasons, Reason{Text: "位于主文件夹，常用位置加权", Weight: 5})
	}
	return score, kind, reasons
}

func penalizedPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	for _, part := range []string{"/library/caches", "/appdata/", "/node_modules/", "/.git/", "/$recycle.bin/", "/.cache/"} {
		if strings.Contains(p, part) {
			return true
		}
	}
	return false
}

func pathDepth(path string) int {
	volume := filepath.VolumeName(path)
	clean := filepath.Clean(path)
	parts := strings.FieldsFunc(clean, func(r rune) bool { return r == '/' || r == '\\' })
	// A volume or UNC share is not a nested folder. On Unix the leading
	// separator is already gone; on Windows filepath.VolumeName names it.
	if volume != "" {
		parts = strings.FieldsFunc(strings.TrimPrefix(filepath.ToSlash(clean), filepath.ToSlash(volume)),
			func(r rune) bool { return r == '/' })
	} else if len(parts) >= 2 && strings.HasPrefix(path, `\\`) {
		parts = parts[2:]
	}
	return len(parts)
}

func pathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	// Fold both spellings on every platform: this is a relevance hint, and
	// macOS's case-insensitive volumes make mixed case common too.
	path, root = strings.ToLower(path), strings.ToLower(root)
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	return strings.HasPrefix(path, strings.TrimSuffix(root, sep)+sep)
}

// Highlight splits name into runs around the query's matches, Everything's
// yellow-mark style: every term's occurrence gets a background. Overlaps
// merge into one run. A case-sensitive query highlights exact matches
// only; a phrase term highlights the whole phrase.
func (q Query) Highlight(name string) []Span {
	return q.highlight(name, false)
}

// HighlightPath is Highlight for a whole path: path: terms mark their
// occurrence in the path, as names do in the list. Plain terms do not
// match the path and are left unmarked, even when the same letters appear
// there.
func (q Query) HighlightPath(path string) []Span {
	return q.highlight(path, true)
}

// highlight marks the terms of one haystack. path selects the path terms;
// name highlighting marks the name terms.
func (q Query) highlight(text string, path bool) []Span {
	if len(q.terms) == 0 {
		return []Span{{Text: text}}
	}
	lower := strings.ToLower(text)
	marks := make([]bool, len(lower))
	any := false
	for _, t := range q.terms {
		needle := t.text
		if (t.kind == termPath) != path {
			continue
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
			hay = text
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
		return []Span{{Text: text}}
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
		b.WriteByte(text[i])
		prev = m
		started = true
	}
	flush(prev)
	return out
}
