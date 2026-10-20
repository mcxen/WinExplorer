package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sunvale/internal/searchquery"
)

func pathLabels(segs []pathSeg) []string {
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		out = append(out, s.Label)
	}
	return out
}

func TestSearchPathSegments(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "alice")
	tests := []struct {
		name       string
		dir, home  string
		maxSegs    int
		wantLabels []string
		wantText   string
	}{
		{
			name:       "home itself",
			dir:        home,
			home:       home,
			maxSegs:    5,
			wantLabels: []string{"主文件夹"},
			wantText:   "主文件夹",
		},
		{
			name:       "deep home path keeps the hit's folder",
			dir:        filepath.Join(home, "Library", "Caches", "com", "app", "very", "deep", "nested", "folder", "structure"),
			home:       home,
			maxSegs:    5,
			wantLabels: []string{"主文件夹", "Library", "…", "folder", "structure"},
			wantText:   "主文件夹 › Library › … › folder › structure",
		},
		{
			name:       "relative child paths",
			dir:        filepath.Join("docs", "reports", "2026"),
			home:       home,
			maxSegs:    5,
			wantLabels: []string{"docs", "reports", "2026"},
			wantText:   "docs › reports › 2026",
		},
		{
			name:       "non-ascii names stay whole",
			dir:        filepath.Join(home, "文档", "季度报告", "最终版本"),
			home:       home,
			maxSegs:    5,
			wantLabels: []string{"主文件夹", "文档", "季度报告", "最终版本"},
			wantText:   "主文件夹 › 文档 › 季度报告 › 最终版本",
		},
		{
			name:       "a long tail keeps more of itself",
			dir:        filepath.Join(home, "a", "b", "c", "d", "e", "f", "g"),
			home:       home,
			maxSegs:    6,
			wantLabels: []string{"主文件夹", "a", "b", "…", "f", "g"},
			wantText:   "主文件夹 › a › b › … › f › g",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			segs := searchPathSegments(tc.dir, tc.home, tc.maxSegs)
			if got := pathLabels(segs); !equalStrings(got, tc.wantLabels) {
				t.Fatalf("labels = %v, want %v", got, tc.wantLabels)
			}
			if got := searchPathText(segs); got != tc.wantText {
				t.Fatalf("text = %q, want %q", got, tc.wantText)
			}
			for _, s := range segs {
				if s.Path == "" {
					t.Fatalf("segment %q has no path", s.Label)
				}
			}
		})
	}
}

func TestSearchPathRoots(t *testing.T) {
	root := string(filepath.Separator)
	if got := searchPathText(searchPathSegments(root, "", 5)); got != sysRootName() {
		t.Fatalf("root = %q, want %q", got, sysRootName())
	}
	unc := `\\server\share\folder\child`
	segs := searchPathSegments(unc, "", 5)
	if len(segs) == 0 || segs[0].Label != `\\server\share` || segs[0].Path != `\\server\share` {
		t.Fatalf("UNC root = %+v", segs)
	}
	if got := searchPathText(segs); got != `\\server\share › folder › child` {
		t.Fatalf("UNC path = %q", got)
	}
	if runtime.GOOS == "windows" {
		segs = searchPathSegments(`C:\Users\alice\Documents`, `C:\Users\alice`, 5)
		if got := searchPathText(segs); got != `主文件夹 › Documents` {
			t.Fatalf("Windows home path = %q", got)
		}
		segs = searchPathSegments(`D:\`, "", 5)
		if len(segs) != 1 || segs[0].Label != "D:" || segs[0].Path != `D:\` {
			t.Fatalf("drive root = %+v", segs)
		}
	}
}

func TestSearchLocationText(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "work", "project")
	tests := []struct {
		name string
		e    Entry
		want string
	}{
		{name: "root itself", e: Entry{Dir: root}, want: "当前文件夹"},
		{
			name: "direct child",
			e:    Entry{Dir: filepath.Join(root, "reports")},
			want: "reports",
		},
		{
			name: "nested child",
			e:    Entry{Dir: filepath.Join(root, "reports", "2026", "q4")},
			want: filepath.Join("reports", "2026", "q4"),
		},
		{
			name: "outside the root",
			e:    Entry{Dir: filepath.Join(root, "..", "elsewhere")},
			want: sysRootName() + " › work › elsewhere",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := searchLocationText(tc.e, root, "")
			if tc.name == "nested child" {
				if got != "reports › 2026 › q4" {
					t.Fatalf("nested location = %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("location = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchNameSpans(t *testing.T) {
	q := searchquery.Parse("report")
	spans := searchNameSpans(q, "report.docx", false, 12)
	var text, ext strings.Builder
	for _, s := range spans {
		text.WriteString(s.Text)
		if s.Text == ".docx" {
			ext.WriteString(s.Text)
		}
	}
	if got := text.String(); got != "report.docx" {
		t.Fatalf("spans concatenate to %q, want the whole name", got)
	}
	if ext.String() != ".docx" {
		t.Fatalf("the extension did not stay a separate span: %+v", spans)
	}
	if spans[len(spans)-1].Color != colorMuted() {
		t.Fatal("the extension span is not muted")
	}
	if spans[0].Background != colorHit() {
		t.Fatal("the queried name run carries no hit mark")
	}

	// A path-only query highlights nothing, but still shows the whole name and
	// mutes the extension.
	spans = searchNameSpans(searchquery.Parse("path:docs"), "notes.txt", false, 12)
	text.Reset()
	for _, s := range spans {
		text.WriteString(s.Text)
	}
	if text.String() != "notes.txt" {
		t.Fatalf("path-only spans = %q", text.String())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
