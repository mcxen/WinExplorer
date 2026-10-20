package searchquery

import (
	"reflect"
	"strings"
	"testing"
	"time"
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
		if q.Matches(n, "/root/"+n, MatchInfo{
			Ext:   strings.ToLower(strings.TrimPrefix(extOfName(n), ".")),
			IsDir: strings.HasSuffix(n, "/"),
		}) {
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
	if !q.Matches("alpha.txt", "/root/docs/alpha.txt", MatchInfo{Ext: "txt"}) {
		t.Fatal("path: should match a hit under docs")
	}
	if q.Matches("alpha.txt", "/root/other/alpha.txt", MatchInfo{Ext: "txt"}) {
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

func TestMetadataFilters(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name string
		q    string
		info MatchInfo
		want bool
	}{
		{"dm today keeps today", "dm:today", MatchInfo{Ext: "txt", ModTime: now.Add(-time.Hour)}, true},
		{"dm today drops yesterday", "dm:today", MatchInfo{Ext: "txt", ModTime: now.Add(-25 * time.Hour)}, false},
		{"dm 7d keeps six days", "dm:7d", MatchInfo{Ext: "txt", ModTime: now.Add(-6 * 24 * time.Hour)}, true},
		{"dm 30d drops forty", "dm:30d", MatchInfo{Ext: "txt", ModTime: now.Add(-40 * 24 * time.Hour)}, false},
		{"dm year keeps january", "dm:year", MatchInfo{Ext: "txt", ModTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)}, true},
		{"dm year drops last year", "dm:year", MatchInfo{Ext: "txt", ModTime: time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local)}, false},
		{"size small", "size:<100kb", MatchInfo{Ext: "txt", Size: 99 * 1000}, true},
		{"size small bound", "size:<100kb", MatchInfo{Ext: "txt", Size: 100 * 1000}, false},
		{"size medium low", "size:100kb..10mb", MatchInfo{Ext: "txt", Size: 100 * 1000}, true},
		{"size medium high", "size:100kb..10mb", MatchInfo{Ext: "txt", Size: 10 * 1000 * 1000}, true},
		{"size large", "size:>10mb", MatchInfo{Ext: "txt", Size: 10*1000*1000 + 1}, true},
		{"size keeps folders", "size:>10mb", MatchInfo{IsDir: true, Size: 0}, true},
		{"kind document", "kind:document", MatchInfo{Ext: "docx"}, true},
		{"kind document misses image", "kind:document", MatchInfo{Ext: "png"}, false},
		{"kind image", "kind:image", MatchInfo{Ext: "heic"}, true},
		{"kind media", "kind:media", MatchInfo{Ext: "mp3"}, true},
		{"kind archive", "kind:archive", MatchInfo{Ext: "7z"}, true},
		{"kind code", "kind:code", MatchInfo{Ext: "go"}, true},
		{"kind code misses folder", "kind:code", MatchInfo{IsDir: true}, false},
		{"unknown dm ignores", "dm:tomorrow", MatchInfo{Ext: "txt", ModTime: now, Size: 1, IsDir: false}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := Parse(tc.q)
			if got := q.Matches("notes", "/root/notes", tc.info); got != tc.want {
				t.Fatalf("%q = %v, want %v", tc.q, got, tc.want)
			}
		})
	}
	if !Parse("kind:image").HasMetadataFilter() || Parse("alpha").HasMetadataFilter() {
		t.Fatal("HasMetadataFilter did not tell the filters apart")
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
	if !q.Matches("xAlPhAx-BETA", "", MatchInfo{}) {
		t.Fatal("alpha matched insensitively before case:")
	}
	if q.Matches("xalpha-beta", "", MatchInfo{}) {
		t.Fatal("case:BETA matched the wrong case")
	}
	// A second case: toggles back.
	q = Parse("case:BETA case:gamma")
	if q.sensitive {
		t.Fatal("double case: did not toggle back")
	}
	if !q.Matches("xxBETAxxgammaxx", "", MatchInfo{}) {
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

func TestHighlightPath(t *testing.T) {
	q := Parse("path:docs alpha")
	path := "/root/docs/alpha/notes.txt"
	got := q.HighlightPath(path)
	if spansText(got) != path {
		t.Fatalf("path highlight text = %q", spansText(got))
	}
	if len(got) != 3 || got[0].Hit || !got[1].Hit || got[1].Text != "docs" || got[2].Hit {
		t.Fatalf("path highlight = %+v", got)
	}
	// A name term is not highlighted in the path, even when the same text
	// appears there.
	got = Parse("alpha").HighlightPath(path)
	if len(got) != 1 || got[0].Hit {
		t.Fatalf("name term marked the path: %+v", got)
	}
	// case: follows the same positional rules in a path.
	got = Parse("case: path:DOCS").HighlightPath("/root/DOCS/notes.txt")
	if len(got) != 3 || !got[1].Hit || got[1].Text != "DOCS" {
		t.Fatalf("case-sensitive path highlight = %+v", got)
	}
}

func spansText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestScore(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	home := "/Users/alice"
	q := Parse("report")
	tests := []struct {
		name, path string
		isDir      bool
		mod        time.Time
		wantKind   MatchKind
		min, max   int
		reason     string
	}{
		{"report.docx", home + "/Documents/project/report.docx", false, now.Add(-time.Hour), MatchName, 5990, 6030, "文件名完全匹配"},
		{"report-final.docx", home + "/Documents/project/report-final.docx", false, now.Add(-time.Hour), MatchName, 3990, 4030, "文件名以“report”开头"},
		{"final-report.docx", home + "/Documents/final-report.docx", false, now.Add(-40 * 24 * time.Hour), MatchName, 2495, 2525, "文件名包含“report”"},
		{"report.json", "/Library/Caches/com.app/report.json", false, now.Add(-time.Hour), MatchName, 5970, 6030, "位于缓存或生成目录，降低排序"},
	}
	pathQuery := Parse("path:report")
	if got, kind, reasons := pathQuery.Score("notes.txt", home+"/Documents/report/notes.txt", false, now, now, home); kind != MatchPath || got == 0 || len(reasons) == 0 || !strings.Contains(reasons[0].Text, "路径命中") {
		t.Fatalf("path-only hit = score %d, kind %v, reasons %+v", got, kind, reasons)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, kind, reasons := q.Score(tc.name, tc.path, tc.isDir, tc.mod, now, home)
			if kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", kind, tc.wantKind)
			}
			if got < tc.min || got > tc.max {
				t.Fatalf("score = %d, want %d..%d (%+v)", got, tc.min, tc.max, reasons)
			}
			found := false
			for _, r := range reasons {
				if strings.Contains(r.Text, tc.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("reasons = %+v, want one about %q", reasons, tc.reason)
			}
		})
	}
	// The deep cache hit stays below the ordinary home hit, as in the
	// design's ordering.
	ordinary, _, _ := q.Score("report.md", home+"/Documents/report.md", false, now, now, home)
	cache, _, _ := q.Score("report.json", "/Library/Caches/com.app/report.json", false, now, now, "")
	if ordinary <= cache {
		t.Fatalf("ordinary score %d is not above cache %d", ordinary, cache)
	}
}
