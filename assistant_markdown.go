package main

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistant_markdown.go draws the assistant's replies as Markdown with mygo's
// ui/markdown: Stream while a reply comes in, View once it is done, in the
// same place so the parse carries on. OpenUI programs are fenced code blocks
// of the reply and are drawn where the model put them; a path in the reply
// is a link that opens it in this window.

// assistMD is one exchange's Markdown view: its options, which keep the
// parse from frame to frame, and the code blocks just copied.
type assistMD struct {
	opts   *markdown.Options
	copied map[int]time.Time
	// turns are the reply lines this frame's source was joined from, for
	// an OpenUI block to find the line it belongs to.
	turns []int
	// last and out cache the source after linkMarkdownPaths.
	last, out string
}

// assistMarkdown builds src, one exchange's reply, into the pane.
func (a *App) assistMarkdown(c *ui.Context, p *assistPaint, src string, turns []int, streaming bool) {
	if p.md == nil {
		p.md = &assistMD{copied: map[int]time.Time{}}
		p.md.opts = a.assistMarkdownOptions(p.md)
	}
	md := p.md
	md.turns = turns
	if src != md.last {
		md.last = src
		md.out = linkMarkdownPaths(normalizeOpenMarkers(src))
	}
	if streaming {
		markdown.Stream(c, md.out, md.opts)
	} else {
		markdown.View(c, md.out, md.opts)
	}
}

func (a *App) assistMarkdownOptions(md *assistMD) *markdown.Options {
	o := &markdown.Options{
		Theme:  &markdown.Theme{FontSize: 13.5},
		OnLink: a.assistOpenLink,
		// The pane is narrow: long lines wrap rather than hide sideways.
		WrapCode: true,
		Reveal:   &markdown.Reveal{Unit: markdown.ByWord},
	}
	o.CodeBlock = func(c *ui.Context, b *markdown.CodeBlockView) { assistCodeBlock(c, md, b) }
	o.Code = map[string]func(c *ui.Context, b *markdown.CodeBlockView){
		"openui": func(c *ui.Context, b *markdown.CodeBlockView) {
			if b.Open {
				assistOpenPlaceholder(c)
				return
			}
			src := strings.TrimSpace(b.Code())
			a.paintOpenBlock(c.Key("oui-"+openBlockKey(src)), src, a.openTurnFor(md.turns, src))
		},
	}
	return o
}

// openTurnFor is the reply line an OpenUI program came from: the one whose
// text holds it, else the last.
func (a *App) openTurnFor(turns []int, src string) int {
	for _, i := range turns {
		if i >= 0 && i < len(a.assist.turns) && strings.Contains(a.assist.turns[i].Text, src) {
			return i
		}
	}
	if len(turns) > 0 {
		return turns[len(turns)-1]
	}
	return -1
}

// openBlockKey names an OpenUI program by its source, so the state of its
// card (sent, the form's values) stays with it across frames and sessions.
func openBlockKey(src string) string {
	h := fnv.New64a()
	h.Write([]byte(strings.TrimSpace(src)))
	return strconv.FormatUint(h.Sum64(), 36)
}

// assistOpenPlaceholder stands for an OpenUI program still being written.
func assistOpenPlaceholder(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(10).Gap(8).Radius(6).Background(colorHover()).AlignItems(ui.Center).
		Children(func() {
			components.Spinner(c, components.SpinnerRing)
			ui.Text(c, "正在生成界面…").FontSize(12).TextColor(colorMuted())
		})
}

// assistCodeBlock is a code block with the language and a 复制 button above
// the code. The library's own block reads its labels from the system
// locale; the pane is Chinese whatever the system says.
func assistCodeBlock(c *ui.Context, md *assistMD, b *markdown.CodeBlockView) {
	box := ui.Column(c).FillWidth().MinWidth(0).Radius(6).Border(1, colorDivider()).
		Background(colorHover()).Clip().Label("代码 " + b.Language)
	box.Children(func() {
		ui.Row(c).FillWidth().Padding(4, 6, 0, 10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, b.Language).FontSize(11).TextColor(colorMuted()).Unselectable()
			ui.Spacer(c)
			at, ok := md.copied[b.Index]
			copied := ok && c.Now().Sub(at) < 2*time.Second
			label, icon := "复制", lucide.Copy()
			if copied {
				label, icon = "已复制", lucide.Check()
			}
			btn := ui.ButtonBase(c).Padding(2, 6).Radius(4).Gap(4).AlignItems(ui.Center).
				Label("复制代码").Disabled(b.Open)
			if btn.Hovered() && !b.Open {
				btn.Background(colorHover())
			}
			btn.Children(func() {
				components.Icon(c, icon).Size(13, 13).TextColor(colorMuted())
				ui.Text(c, label).FontSize(11).TextColor(colorMuted())
			})
			if btn.Clicked() {
				c.WriteClipboard(b.Code())
				md.copied[b.Index] = c.Now()
				c.After(2 * time.Second)
			}
		})
		b.Body(c)
	})
}

// assistOpenLink opens a link of a reply: a file or folder in this window,
// a web address in the browser.
func (a *App) assistOpenLink(url string) {
	if p, ok := linkPath(url); ok {
		a.assistOpenPath(p)
		return
	}
	lower := strings.ToLower(url)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
		if err := sysOpen(url); err != nil {
			a.statusMsg = "无法打开链接：" + err.Error()
		}
	}
}

// assistOpenPath goes to a folder, or to the folder of a file with the file
// selected. A path that is gone says so instead of failing quietly.
func (a *App) assistOpenPath(p string) {
	info, err := os.Stat(p)
	if err != nil {
		a.statusMsg = "找不到 " + p
		return
	}
	a.openPinned(p, info.IsDir())
}

// linkPath is the local path a link names: a file:// address, an absolute
// path, or one under ~.
func linkPath(url string) (string, bool) {
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

// normalizeOpenMarkers turns the ]]>openui:content … ]]>openui:end form into
// an ```openui fence, the one form the Markdown view draws.
func normalizeOpenMarkers(src string) string {
	if !strings.Contains(src, openUIMarkerOpen) {
		return src
	}
	src = strings.ReplaceAll(src, openUIMarkerOpen, "\n```openui\n")
	return strings.ReplaceAll(src, openUIMarkerEnd, "\n```\n")
}

var (
	codePathRe = regexp.MustCompile("`((?:[A-Za-z]:[\\\\/]|~/|/)[^`\\n]*)`")
	barePathRe = regexp.MustCompile(`(^|[\s(（：:，,])((?:[A-Za-z]:\\|~/)[^\s<>|"*?` + "`" + `，。；）)]+|/[^\s/<>|"*?` + "`" + `，。；）)]+(?:/[^\s<>|"*?` + "`" + `，。；）)]+)+)`)
)

// linkMarkdownPaths makes the paths a reply names into links: `C:\x` and
// `/home/x` in code, and bare C:\x, ~/x and /a/b in prose. Code blocks and
// existing links are left alone.
func linkMarkdownPaths(src string) string {
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
