package assistant

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LinkPath is the local path a link names: a file:// address, an absolute
// path, or one under ~.
func LinkPath(url string) (string, bool) {
	u := strings.TrimSpace(url)
	if strings.HasPrefix(strings.ToLower(u), "file://") {
		u = u[len("file://"):]
		// file:///C:/x has a slash before the drive.
		if len(u) > 2 && u[0] == '/' && u[2] == ':' {
			u = u[1:]
		}
		u = strings.ReplaceAll(u, "%20", " ")
	}
	if strings.HasPrefix(u, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			u = filepath.Join(home, u[2:])
		}
	}
	if winAbs(u) || strings.HasPrefix(u, "/") {
		return filepath.Clean(filepath.FromSlash(u)), true
	}
	return "", false
}

func winAbs(s string) bool {
	return len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') &&
		(s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z')
}

// NormalizeOpenMarkers turns the ]]>openui:content … ]]>openui:end form into
// an ```openui fence, the one form the Markdown view draws.
func NormalizeOpenMarkers(src string) string {
	if !strings.Contains(src, OpenUIMarkerOpen) {
		return src
	}
	src = strings.ReplaceAll(src, OpenUIMarkerOpen, "\n```openui\n")
	return strings.ReplaceAll(src, OpenUIMarkerEnd, "\n```\n")
}

var (
	codePathRe = regexp.MustCompile("`((?:[A-Za-z]:[\\\\/]|~/|/)[^`\\n]*)`")
	barePathRe = regexp.MustCompile(`(^|[\s(（：:，,])((?:[A-Za-z]:\\|~/)[^\s<>|"*?` + "`" + `，。；）)]+|/[^\s/<>|"*?` + "`" + `，。；）)]+(?:/[^\s<>|"*?` + "`" + `，。；）)]+)+)`)
)

// LinkMarkdownPaths makes the paths a reply names into links: `C:\x` and
// `/home/x` in code, and bare C:\x, ~/x and /a/b in prose. Code blocks and
// existing links are left alone.
func LinkMarkdownPaths(src string) string {
	if !strings.ContainsAny(src, "/\\") {
		return src
	}
	lines := strings.Split(src, "\n")
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fence = trim[:3]
			continue
		}
		if strings.Contains(line, "](") || strings.Contains(line, "://") {
			continue
		}
		lines[i] = linkLinePaths(line)
	}
	return strings.Join(lines, "\n")
}

func linkLinePaths(line string) string {
	// Code spans first, then the prose between them.
	var b strings.Builder
	rest := line
	for {
		loc := codePathRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			b.WriteString(linkBarePaths(rest))
			return b.String()
		}
		b.WriteString(linkBarePaths(rest[:loc[0]]))
		p := rest[loc[2]:loc[3]]
		b.WriteString("[`" + p + "`](" + pathURL(p) + ")")
		rest = rest[loc[1]:]
	}
}

func linkBarePaths(s string) string {
	if strings.Count(s, "`") > 0 {
		return s // inside or around code the span rules decide
	}
	return barePathRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := barePathRe.FindStringSubmatch(m)
		lead, p := sub[1], sub[2]
		return lead + "[" + escapeMarkdown(p) + "](" + pathURL(p) + ")"
	})
}

func pathURL(p string) string {
	u := strings.ReplaceAll(p, "\\", "/")
	if winAbs(p) {
		u = "/" + u
	}
	if strings.HasPrefix(p, "~/") {
		return "<" + p + ">"
	}
	return "<file://" + u + ">"
}

func escapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>#|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
