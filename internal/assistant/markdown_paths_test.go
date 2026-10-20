package assistant

import (
	"strings"
	"testing"
)

func TestLinkMarkdownPaths(t *testing.T) {
	cases := []struct{ in, want string }{
		{"看 `C:\\Users\\a\\下载`", "看 [`C:\\Users\\a\\下载`](<file:///C:/Users/a/下载>)"},
		{"用 `C:\\Program Files\\Sunvale\\资料`", "用 [`C:\\Program Files\\Sunvale\\资料`](<file:///C:/Program Files/Sunvale/资料>)"},
		{"在 /home/a/文档 里", "在 [/home/a/文档](<file:///home/a/文档>) 里"},
		// Bare prose paths stop at spaces today; callers wanting a Windows
		// path with spaces to be one link wrap it in a code span.
		{"C:\\Program Files\\Sunvale", "[C:\\\\Program](<file:///C:/Program>) Files\\Sunvale"},
		{"比例 1/2 不是路径", "比例 1/2 不是路径"},
		{"[已有](https://x.test/a/b)", "[已有](https://x.test/a/b)"},
		{"```\n/etc/hosts\n```", "```\n/etc/hosts\n```"},
		{"放到 ~/归档。", "放到 [~/归档](<~/归档>)。"},
	}
	for _, c := range cases {
		if got := LinkMarkdownPaths(c.in); got != c.want {
			t.Errorf("LinkMarkdownPaths(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for url, want := range map[string]string{
		"file:///home/a/x": "/home/a/x",
		"/tmp/x":           "/tmp/x",
	} {
		if got, ok := LinkPath(url); !ok || got != want {
			t.Errorf("LinkPath(%q) = %q %v", url, got, ok)
		}
	}
	if _, ok := LinkPath("https://go.dev"); ok {
		t.Error("a web address read as a path")
	}
}

func TestNormalizeOpenMarkers(t *testing.T) {
	if !strings.Contains(NormalizeOpenMarkers("a]]>openui:content\nx\n]]>openui:end"), "```openui") {
		t.Fatal("markers were not made a fence")
	}
}
