package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistant_openui_paint.go draws a parsed OpenUI program with the pane's
// own controls. A card answers once: the first button sent locks it and the
// card then says what was chosen. A button only posts the next message; the
// model still calls a tool, under the usual confirmations, before any file
// changes. A danger button asks again in place before it sends.

// assistOpenState is what a window keeps of its OpenUI cards between
// frames. None of it is saved: what a card was answered with is, on the
// turn (aiTurn.OpenDone).
type assistOpenState struct {
	vals    map[string]*string
	src     map[string]bool
	confirm string
	custom  map[int]*string
	planNo  map[string]bool
	tree    map[string]bool
	// charts keeps each chart's selection by card key and kind.
	charts map[string]*chart.State
	// keys are the Alt+number shortcuts of the latest card still open,
	// gathered as the transcript builds.
	keys []openShortcut
}

type openShortcut struct {
	label   string
	primary bool
	run     func()
}

func openButtonVariant(v string) components.ButtonVariant {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "primary":
		return components.ButtonPrimary
	case "danger", "destructive":
		return components.ButtonOutline
	case "ghost", "link":
		return components.ButtonGhost
	case "outline":
		return components.ButtonOutline
	default:
		return components.ButtonSecondary
	}
}

// openPaint is one card being drawn.
type openPaint struct {
	turn   int
	key    string
	locked bool
	chosen string
	form   *ouiView
	keys   []openShortcut
}

var openDigitKeys = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// assistantOpenShortcuts runs the shortcut pressed for the latest open card
// or choice: Alt+1…9 its buttons or options, Alt+Enter its main button.
func (a *App) assistantOpenShortcuts(c *ui.Context) {
	keys := a.assist.open.keys
	if len(keys) == 0 || a.assist.running {
		return
	}
	for i, k := range openDigitKeys {
		if i < len(keys) && c.Shortcut(ui.Alt, k) {
			keys[i].run()
			return
		}
	}
	if c.Shortcut(ui.Alt, ui.KeyEnter) {
		for _, k := range keys {
			if k.primary {
				k.run()
				return
			}
		}
	}
}

func (a *App) paintOpenBlock(c *ui.Context, src string, turn int) {
	key := openBlockKey(src)
	view := parseOpenUI(src)
	if view == nil {
		a.paintOpenFailed(c, src, key)
		return
	}
	p := &openPaint{turn: turn, key: key}
	p.chosen = a.openChosen(view, turn, key)
	p.locked = p.chosen != ""
	ui.Column(c).FillWidth().MinWidth(0).Gap(6).Children(func() {
		a.paintOpenView(c, view, p, 0)
		if p.locked {
			openChosenNote(c, p.chosen)
		} else if len(p.keys) > 1 && !a.assist.running {
			ui.Text(c, "Alt+1 至 Alt+"+itoa(min(len(p.keys), 9))+" 选择").FontSize(11).TextColor(colorMuted())
		}
	})
	if !p.locked {
		a.assist.open.keys = p.keys
	}
}

// openChosenNote says what answered a card, with a drawn check mark.
func openChosenNote(c *ui.Context, chosen string) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		components.Icon(c, lucide.Check()).Size(13, 13).TextColor(colorMuted())
		ui.Text(c, "已选择「"+openShort(chosen, 40)+"」").FontSize(12).TextColor(colorMuted()).Wrap()
	})
}

// openChosen is the label that answered the card: OpenDone, or for a
// session saved before it, a button whose action was sent.
func (a *App) openChosen(view *ouiView, turn int, key string) string {
	if turn < 0 || turn >= len(a.assist.turns) {
		return ""
	}
	t := a.assist.turns[turn]
	if done := t.OpenDone[key]; done != "" {
		return done
	}
	var found string
	var walk func(v *ouiView)
	walk = func(v *ouiView) {
		if v == nil || found != "" {
			return
		}
		if v.Kind == "button" {
			for _, s := range t.OpenSent {
				if s == v.Action {
					found = v.Label
					return
				}
			}
		}
		for _, ch := range v.Children {
			walk(ch)
		}
	}
	walk(view)
	return found
}

// paintOpenFailed is a program the pane could not draw: say so, and let
// the user read what the model wrote.
func (a *App) paintOpenFailed(c *ui.Context, src, key string) {
	st := &a.assist.open
	ui.Column(c).FillWidth().MinWidth(0).Gap(4).Padding(8).Radius(innerRadius()).Background(colorHover()).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.TriangleAlert()).Size(14, 14).TextColor(colorMuted())
			ui.Text(c, "这块界面没能画出来").FontSize(12).TextColor(colorMuted()).Grow(1).MinWidth(0)
			label := "查看源码"
			if st.src[key] {
				label = "收起源码"
			}
			if components.Button(c, label, components.ButtonGhost).Clicked() {
				if st.src == nil {
					st.src = map[string]bool{}
				}
				st.src[key] = !st.src[key]
			}
		})
		if st.src[key] {
			ui.Text(c, src).FontSize(11).Font("monospace").TextColor(colorText()).Wrap().Selectable()
		}
	})
}

func (a *App) paintOpenView(c *ui.Context, v *ouiView, p *openPaint, depth int) {
	if v == nil || depth > 12 {
		return
	}
	switch v.Kind {
	case "stack":
		var box ui.Element
		if v.Row {
			box = ui.Row(c).FillWidth().MinWidth(0).Wrap().Gap(v.Gap).AlignItems(ui.Center)
		} else {
			box = ui.Column(c).FillWidth().MinWidth(0).Gap(v.Gap)
		}
		box.Children(func() {
			for i, ch := range v.Children {
				a.paintOpenView(c.Key(i), ch, p, depth+1)
			}
		})
	case "card":
		ui.Column(c).FillWidth().MinWidth(0).Padding(10).Gap(8).Radius(8).Border(1, colorDivider()).
			Background(colorSurface()).Label("OpenUI card").Children(func() {
			for i, ch := range v.Children {
				a.paintOpenView(c.Key(i), ch, p, depth+1)
			}
		})
	case "header":
		ui.Column(c).FillWidth().MinWidth(0).Gap(2).Children(func() {
			if v.Text != "" {
				ui.Text(c.Key("title"), v.Text).FontSize(14).FontWeight(600).TextColor(colorText()).Wrap().Selectable()
			}
			if v.Sub != "" {
				ui.Text(c.Key("sub"), v.Sub).FontSize(12).TextColor(colorMuted()).Wrap().Selectable()
			}
		})
	case "text":
		size, heavy, muted := openTextStyle(v.Size)
		el := ui.Text(c, v.Text).FontSize(size).TextColor(colorText()).Wrap().Selectable()
		if muted {
			el.TextColor(colorMuted())
		}
		if heavy {
			el.FontWeight(600)
		}
	case "table":
		a.paintOpenTable(c, v)
	case "tree":
		a.paintOpenTree(c, v, p)
	case "form":
		outer := p.form
		p.form = v
		ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
			for i, ch := range v.Children {
				a.paintOpenView(c.Key(i), ch, p, depth+1)
			}
		})
		p.form = outer
	case "input", "select", "number", "path", "time", "date", "days", "switch", "segmented",
		"multi_select", "filetypes", "tags", "slider", "progress", "table_select",
		"list", "image", "video", "rating":
		a.paintOpenField(c, v, p)
	case "button":
		a.paintOpenButton(c, v, p)
	case "note", "badge", "keyvalue", "chart", "donut", "accordion", "divider":
		a.paintOpenExtra(c, v, p, depth)
	case "medialist":
		a.paintOpenMediaList(c, v)
	case "files", "steps", "stats", "gallery", "taskprogress":
		a.paintOpenMore(c, v)
	}
}

// paintOpenExtra draws the OPENUI4 presentation components. They are laid
// out by the same stack the fields use, so a card mixes them freely.
func (a *App) paintOpenExtra(c *ui.Context, v *ouiView, p *openPaint, depth int) {
	if v == nil {
		return
	}
	switch v.Kind {
	case "note":
		a.paintOpenNote(c, v)
	case "badge":
		components.Badge(c, v.Text)
	case "keyvalue":
		a.paintOpenKeyValue(c, v)
	case "chart":
		a.paintOpenChart(c, v, p)
	case "donut":
		a.paintOpenDonut(c, v, p)
	case "accordion":
		a.paintOpenAccordion(c, v, p, depth)
	case "divider":
		ui.Box(c).FillWidth().Height(1).Background(colorDivider())
	}
}

// openToneColor is the theme color a tone names; neutral answers the muted
// token, which notes and key values paint with.
func openToneColor(tone string) ui.Color {
	switch tone {
	case "success":
		return tokSuccess
	case "warning":
		return tokWarning
	case "danger":
		return tokDanger
	case "info":
		return colorAccent()
	default:
		return colorMuted()
	}
}

// openToneIcon is the glyph a tone implies when the program named none.
func openToneIcon(tone string) *ui.SVG {
	switch tone {
	case "success":
		return lucide.CircleCheck()
	case "warning":
		return lucide.TriangleAlert()
	case "danger":
		return lucide.CircleAlert()
	case "info":
		return lucide.Info()
	default:
		return nil
	}
}

// openNamedIcon draws one of the supported lucide glyphs by its OpenUI name.
// Returns false for a name outside the set (or none).
func openNamedIcon(c *ui.Context, name, tone string, w, h float32) bool {
	var svg *ui.SVG
	switch name {
	case "info":
		svg = lucide.Info()
	case "lightbulb":
		svg = lucide.Lightbulb()
	case "sparkles":
		svg = lucide.Sparkles()
	case "circlecheck":
		svg = lucide.CircleCheck()
	case "circlealert":
		svg = lucide.CircleAlert()
	case "trianglealert":
		svg = lucide.TriangleAlert()
	case "circlex":
		svg = lucide.CircleX()
	case "ban":
		svg = lucide.Ban()
	case "check":
		svg = lucide.Check()
	case "clock":
		svg = lucide.Clock()
	case "star":
		svg = lucide.Star()
	case "tag":
		svg = lucide.Tag()
	case "tags":
		svg = lucide.Tags()
	case "folder":
		svg = lucide.Folder()
	case "file":
		svg = lucide.File()
	case "filetext":
		svg = lucide.FileText()
	case "image":
		svg = lucide.Image()
	case "video":
		svg = lucide.Video()
	case "music":
		svg = lucide.Music()
	case "archive":
		svg = lucide.Archive()
	case "harddrive":
		svg = lucide.HardDrive()
	case "gauge":
		svg = lucide.Gauge()
	case "activity":
		svg = lucide.Activity()
	case "zap":
		svg = lucide.Zap()
	case "shieldcheck":
		svg = lucide.ShieldCheck()
	case "shieldalert":
		svg = lucide.ShieldAlert()
	case "chartpie":
		svg = lucide.ChartPie()
	case "chartcolumn":
		svg = lucide.ChartColumn()
	case "chartline":
		svg = lucide.ChartLine()
	case "layers":
		svg = lucide.Layers()
	case "dot":
		svg = lucide.Dot()
	case "pin":
		svg = lucide.Pin()
	case "quote":
		svg = lucide.Quote()
	default:
		return false
	}
	components.Icon(c, svg).Size(w, h).TextColor(openToneColor(tone))
	return true
}

// paintOpenNote draws a callout strip: a tinted icon (the tone's own when the
// program named none), the text, and a hairline colored by the same tone.
func (a *App) paintOpenNote(c *ui.Context, v *ouiView) {
	tint := openToneColor(v.Tone)
	if v.Tone == "" {
		tint = colorAccent()
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Padding(8, 10).
		Radius(6).BorderStartWidth(3).BorderColor(tint).Background(colorSurface()).Children(func() {
		if !openNamedIcon(c.Key("icon"), v.Icon, v.Tone, 15, 15) {
			if svg := openToneIcon(v.Tone); svg != nil {
				components.Icon(c, svg).Size(15, 15).TextColor(tint)
			} else {
				components.Icon(c, lucide.Info()).Size(15, 15).TextColor(tint)
			}
		}
		ui.Text(c, v.Text).FontSize(12).TextColor(colorText()).Wrap().Grow(1).MinWidth(0).Selectable()
	})
}

// paintOpenKeyValue draws one summary row: label left, value right, the
// value tinted by the tone when one was asked for.
func (a *App) paintOpenKeyValue(c *ui.Context, v *ouiView) {
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, v.Label).FontSize(12).TextColor(colorMuted()).Wrap().MaxWidth(160).Shrink(0)
		ui.Spacer(c)
		ui.Text(c, v.Text).FontSize(12).FontWeight(600).TextColor(openToneColor(v.Tone)).Wrap().
			Grow(1).MinWidth(0).TextAlign(ui.End).Selectable()
	})
}
