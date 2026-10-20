package searchquery

import (
	"reflect"
	"strings"
	"testing"
)

// searchquery_test.go drives the query language on its own: parsing,
// matching and highlighting, each against plain string fixtures — no walk,
// no UI. The walk's own behavior stays with the app's recursive search
// tests.

// matchNames runs the query over names and returns those that matched.
// path is synthesized as /root/<name> so path: has something to find.
func matchNames(q Query, names ...string) []string {
	var out []string
	for _, n := range names {
		if q.Matches(n, "/root/"+n, strings.ToLower(strings.TrimPrefix(extOfName(n), ".")), strings.HasSuffix(n, "/")) {
			out = append(out, strings.TrimSuffix(n, "/"))
		}
	}
	return out
}

// extOfName is the test's ext helper.
func extOfName(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

func TestParseBasics(t *testing.T) {
	// A plain query is one substring term.
	q := Parse("alpha")
	if len(q.terms) != 1 || q.terms[0].text != "alpha" || q.terms[0].kind != termName {
		t.Fatalf("terms = %+v", q.terms)
	}
	if q.sensitive || q.filtering != filterAny || q.exts != nil {
		t.Fatalf("plain query grew filters: %+v", q)
	}

	// Spaces are AND; quoted phrases survive as one term.
	q = Parse(`报告 "final draft" 2024`)
	if len(q.terms) != 3 || q.terms[1].text != "final draft" {
		t.Fatalf("AND terms = %+v", q.terms)
	}
	if got := matchNames(q, "报告 final draft 2024.pdf", "报告 final.pdf"); len(got) != 1 {
		t.Fatalf("AND matched %v", got)
	}

	// file: / folder: / ext:.
	q = Parse("file:alpha")
	if q.filtering != filterFile {
		t.Fatal("file: did not set the file filter")
	}
	if got := matchNames(q, "alpha.txt", "alpha/"); len(got) != 1 || got[0] != "alpha.txt" {
		t.Fatalf("file: matched %v", got)
	}
	q = Parse("folder:alpha")
	if q.filtering != filterFolder {
		t.Fatal("folder: did not set the folder filter")
	}
	if got := matchNames(q, "alpha.txt", "alpha/"); len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("folder: matched %v", got)
	}
	q = Parse("ext:txt;md alpha")
	if !reflect.DeepEqual(q.exts, []string{"txt", "md"}) {
		t.Fatalf("exts = %v", q.exts)
	}
	if got := matchNames(q, "alpha.txt", "alpha.md", "alpha.bin"); len(got) != 2 {
		t.Fatalf("ext: matched %v", got)
	}
	q = Parse("ext:.txt")
	if !reflect.DeepEqual(q.exts, []string{"txt"}) {
		t.Fatalf("ext with dot = %v", q.exts)
	}

	// path: matches the whole path, and does not highlight the name.
	q = Parse("path:docs alpha")
	if len(q.terms) != 2 || q.terms[0].kind != termPath || q.terms[1].kind != termName {
		t.Fatalf("path terms = %+v", q.terms)
	}
	if !q.Matches("alpha.txt", "/root/docs/alpha.txt", "txt", false) {
		t.Fatal("path: should match a hit under docs")
	}
	if q.Matches("alpha.txt", "/root/other/alpha.txt", "txt", false) {
		t.Fatal("path: matched outside docs")
	}

	// An unmatched open quote still yields its text.
	q = Parse(`"open phrase`)
	if len(q.terms) != 1 || q.terms[0].text != "open phrase" {
		t.Fatalf("open quote terms = %+v", q.terms)
	}
}

func TestParseModifiersAreNotEaten(t *testing.T) {
	// Drive letters, URLs and Windows paths are ordinary terms.
	for _, s := range []string{`D:\work`, `http://example.com`, `C:盘资料`, "alpha-beta"} {
		q := Parse(s)
		if len(q.terms) != 1 || q.terms[0].text != s {
			t.Fatalf("%q parsed to %+v", s, q.terms)
		}
	}
	// Unknown modifier words are ordinary terms too — the engine only knows
	// what it can honestly answer.
	q := Parse("regex:ab.* dupe:yes")
	if len(q.terms) != 2 || q.terms[0].text != "regex:ab.*" {
		t.Fatalf("unknown modifiers = %+v", q.terms)
	}
	// file: alone (no term) still filters, matching Everything.
	q = Parse("file:")
	if q.filtering != filterFile || len(q.terms) != 0 {
		t.Fatalf("bare file: = %+v", q)
	}
}

func TestCaseModifier(t *testing.T) {
	// case: toggles sensitivity from its position to the end.
	q := Parse("alpha case:BETA")
	if len(q.terms) != 2 {
		t.Fatalf("terms = %+v", q.terms)
	}
	// The term before case: stays insensitive: xAlPhAx alone does not
	// match because BETA must also match, but the insensitive alpha term
	// accepts any case of it.
	if !q.Matches("xAlPhAx-BETA", "", "", false) {
		t.Fatal("alpha matched insensitively before case:")
	}
	if q.Matches("xalpha-beta", "", "", false) {
		t.Fatal("case:BETA matched the wrong case")
	}
	// A second case: toggles back.
	q = Parse("case:BETA case:gamma")
	if q.sensitive {
		t.Fatal("double case: did not toggle back")
	}
	if !q.Matches("xxBETAxxgammaxx", "", "", false) {
		t.Fatal("toggled-back query should match insensitively")
	}
}

func TestHighlight(t *testing.T) {
	// One plain term: each occurrence marked, adjacent ones merging
	// (banana's two "an"s share the middle "a" — one merged run).
	q := Parse("an")
	got := q.Highlight("banana")
	if spansText(got) != "banana" {
		t.Fatalf("highlight text = %q", spansText(got))
	}
	if len(got) != 3 || !got[1].Hit || got[1].Text != "anan" || got[0].Hit || got[2].Hit {
		t.Fatalf("banana spans = %+v", got)
	}

	// Case-insensitive by default: the marks follow the original text.
	q = Parse("alpha")
	got = q.Highlight("x-ALPHA-y")
	if spansText(got) != "x-ALPHA-y" || len(got) != 3 || !got[1].Hit || got[1].Text != "ALPHA" {
		t.Fatalf("insensitive highlight = %+v", got)
	}

	// case: highlights only exact-case matches: ALPHA lights up, the
	// lowercase alpha beside it does not.
	q = Parse("case:ALPHA")
	got = q.Highlight("x-ALPHA-y z-alpha")
	if len(got) != 3 || !got[1].Hit || got[1].Text != "ALPHA" || got[2].Hit {
		t.Fatalf("case highlight = %+v", got)
	}

	// Adjacent terms merge into one continuous run.
	q = Parse("al lph")
	got = q.Highlight("alpha")
	if len(got) != 2 || !got[0].Hit || got[0].Text != "alph" || got[1].Hit {
		t.Fatalf("merge spans = %+v", got)
	}

	// A name with no match (a path: hit) comes back unhighlighted.
	q = Parse("path:docs alpha")
	got = q.Highlight("report.txt")
	if len(got) != 1 || got[0].Hit || got[0].Text != "report.txt" {
		t.Fatalf("unmatched highlight = %+v", got)
	}
}

func spansText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
