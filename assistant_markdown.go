package main

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui/markdown/parser"

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
	// Colors are resolved per frame so an appearance change does not leave a
	// stale Markdown palette behind.
	md.opts.Theme = assistMarkdownTheme()
	if streaming {
		markdown.Stream(c, md.out, md.opts)
	} else {
		markdown.View(c, md.out, md.opts)
	}
}

// assistMarkdownTheme keeps the Markdown blocks on the app's tokens. Quote
// backgrounds are painted by assistQuoteBlock because markdown.Theme only
// exposes the quote bar and text.
func assistMarkdownTheme() *markdown.Theme {
	return &markdown.Theme{
		FontSize:            13.5,
		Text:                colorText(),
		Muted:               colorMuted(),
		Link:                colorAccent(),
		Strong:              colorText(),
		Code:                colorText(),
		CodeBackground:      colorHover(),
		QuoteBar:            colorDivider(),
		QuoteText:           colorMuted(),
		Rule:                colorDivider(),
		CodeBlockBackground: colorSurface(),
		CodeBlockBorder:     colorDivider(),
		TableBorder:         colorDivider(),
		TableHeader:         colorHover(),
		Mark:                colorHit(),
		Checkbox:            colorAccent(),
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
	o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		return assistQuoteBlock(c, d, i, o)
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
// Its 64-DIP floor is the height of the smallest card, so closing the fence
// swaps the placeholder for a card without moving the reply below it.
func assistOpenPlaceholder(c *ui.Context) {
	ui.Row(c).FillWidth().MinHeight(64).Padding(10).Gap(8).Radius(8).Border(1, colorDivider()).
		Background(colorSurface()).AlignItems(ui.Center).Label("OpenUI placeholder").
		Children(func() {
			components.Spinner(c, components.SpinnerRing)
			ui.Text(c, "正在生成界面…").FontSize(12).TextColor(colorMuted())
		})
}

// assistQuoteBlock gives quote blocks the same raised surface and divider as
// cards. markdown.Theme has no quote background, so render the quote's own
// source in a surface box and let the nested view keep all Markdown styling.
func assistQuoteBlock(c *ui.Context, d *parser.Document, i int, o *markdown.Options) bool {
	b := d.Block(i)
	if b == nil || b.Kind != parser.Quote {
		return false
	}
	src := stripQuoteMarkers(d.String()[b.Start:b.End])
	ui.Column(c).FillWidth().MinWidth(0).Padding(8, 10).Radius(6).BorderStartWidth(3).
		BorderColor(colorDivider()).Background(colorSurface()).Children(func() {
		markdown.View(c, src, o)
	})
	return true
}

// stripQuoteMarkers removes one quote marker per line; a nested quote keeps
// its marker and is styled by the nested Markdown view.
func stripQuoteMarkers(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		line = strings.TrimLeft(line, " ")
		if strings.HasPrefix(line, ">") {
			line = strings.TrimPrefix(line, ">")
			line = strings.TrimPrefix(line, " ")
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// assistCopyFeedbackDuration matches mygo's ordinary toast lifetime. The
// inline "已复制" label is the copy toast's counterpart and must not turn back
// before or after that toast.
const assistCopyFeedbackDuration = 4 * time.Second

// assistCodeBlock is a code block with the language and a 复制 button above
// the code. The library's own block reads its labels from the system
// locale; the pane is Chinese whatever the system says.
func assistCodeBlock(c *ui.Context, md *assistMD, b *markdown.CodeBlockView) {
	box := ui.Column(c).FillWidth().MinWidth(0).Radius(6).Border(1, colorDivider()).
		Background(colorSurface()).Clip().Label("代码 " + b.Language)
	box.Children(func() {
		ui.Row(c).FillWidth().Padding(4, 6, 0, 10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, b.Language).FontSize(11).TextColor(colorMuted()).Unselectable()
			ui.Spacer(c)
			at, ok := md.copied[b.Index]
			copied := ok && c.Now().Sub(at) < assistCopyFeedbackDuration
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
				c.After(assistCopyFeedbackDuration)
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
