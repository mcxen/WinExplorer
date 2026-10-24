package main

import (
	"strconv"
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
	ui.Column(c).FillWidth().MinWidth(0).Gap(4).Padding(8).Radius(6).Background(colorHover()).Children(func() {
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

// openChartPalette is the series palette, theme-aware, shared by the two
// chart kinds (the volume donut keeps its own richer one).
func openChartPalette() []ui.Color {
	return []ui.Color{colorAccent(), tokWarning, tokSuccess, ui.Hex("#8B5CF6")}
}

// paintOpenChart draws Chart on Onevra's grouped bars, PlotHeight 140 for the
// pane's width, the legend kept (a chart without its series names answers
// less than the table beside it). Selection state lives in the card's state
// so two charts on one card do not share a cursor.
func (a *App) paintOpenChart(c *ui.Context, v *ouiView, p *openPaint) {
	series := make([]chart.Series, 0, len(v.Series))
	for i, s := range v.Series {
		label := s.Label
		if label == "" {
			label = "系列 " + itoa(i+1)
		}
		series = append(series, chart.Series{ID: "s" + itoa(i), Label: label, Values: s.Values})
	}
	data := chart.Data{Labels: v.Options, Series: series}
	ui.Box(c).FillWidth().MinWidth(0).Children(func() {
		chart.Bar(c, a.openChartState(p, "bar"), data, chart.Options{
			Label:       "图表",
			PlotHeight:  140,
			FormatValue: openChartFormat,
		})
	})
}

// paintOpenDonut draws DonutChart: each series' first value is a slice. The
// legend is the pane's own two-column list — name and value — which stays
// readable at the pane's width where the chart's own legend wraps.
func (a *App) paintOpenDonut(c *ui.Context, v *ouiView, p *openPaint) {
	palette := openChartPalette()
	slices := make([]chart.Slice, 0, len(v.Series))
	values := make([]float64, 0, len(v.Series))
	total := 0.0
	for i, s := range v.Series {
		val := s.Values[0]
		if val < 0 {
			val = 0
		}
		label := s.Label
		if label == "" {
			label = "项 " + itoa(i+1)
		}
		slices = append(slices, chart.Slice{ID: "d" + itoa(i), Label: label, Value: val})
		values = append(values, val)
		total += val
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
		chart.Donut(c, a.openChartState(p, "donut"), slices, chart.Options{
			Label:       "占比",
			PlotHeight:  128,
			HideLegend:  true,
			FormatValue: openChartFormat,
			Detail:      func(*ui.Context, chart.Selection) {},
		})
		for i, s := range slices {
			share := 0.0
			if total > 0 {
				share = s.Value / total
			}
			ui.Row(c.Key("slice-" + s.ID)).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Draw(func(painter *ui.Painter, r ui.Rect) {
					painter.Fill(r, palette[i%len(palette)], r.H/2)
				})
				ui.Text(c, s.Label).FontSize(12).TextColor(colorText()).Grow(1).MinWidth(0).Wrap().Selectable()
				ui.Text(c, openChartFormat(values[i])).FontSize(12).TextColor(colorMuted()).Shrink(0).Selectable()
				if share > 0 {
					ui.Text(c, itoa(int(share*100+0.5))+"%").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
				}
			})
		}
	})
}

// openChartFormat prints chart figures: integers plain, the rest to one
// decimal, so axis ticks and legend values agree.
func openChartFormat(v float64) string {
	if v == float64(int64(v)) {
		return itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// openChartState keeps one chart's selection per card and kind, so picking a
// bar of one chart does not move another.
func (a *App) openChartState(p *openPaint, kind string) *chart.State {
	st := &a.assist.open
	if st.charts == nil {
		st.charts = map[string]*chart.State{}
	}
	id := p.key + "\x00" + kind
	if st.charts[id] == nil {
		st.charts[id] = &chart.State{Index: -1}
	}
	return st.charts[id]
}

// paintOpenAccordion draws Accordion on Onevra's Accordion: the pane's own
// disclosure state, the first item open when nothing was asked.
func (a *App) paintOpenAccordion(c *ui.Context, v *ouiView, p *openPaint, depth int) {
	st := &a.assist.open
	if st.planNo == nil {
		st.planNo = map[string]bool{}
	}
	components.Accordion(c, func(group *components.AccordionGroup) {
		for i, item := range v.Items {
			id := p.key + "\x00acc\x00" + itoa(i) + "\x00" + item.Text
			open := item.Open
			if seen, ok := st.planNo[id]; ok {
				open = seen
			} else if i == 0 {
				open = true
			}
			group.Item(item.Text, &open, func() {
				for j, ch := range item.Children {
					a.paintOpenView(c.Key(j), ch, p, depth+1)
				}
			})
			st.planNo[id] = open
		}
	})
}

// paintOpenTree draws the read-only directory tree. The disclosure is a
// separate button so clicking a node with a path can navigate without also
// changing its expanded state.
func (a *App) paintOpenTree(c *ui.Context, v *ouiView, p *openPaint) {
	if v == nil {
		return
	}
	st := &a.assist.open
	if st.tree == nil {
		st.tree = map[string]bool{}
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(1).Children(func() {
		a.paintOpenTreeItems(c, v, p, 0)
	})
}

func (a *App) paintOpenTreeItems(c *ui.Context, parent *ouiView, p *openPaint, depth int) {
	for i, item := range parent.Items {
		id := treeItemKey(p.key, item, depth, i)
		keyed := c.Key(id)
		hasKids := len(item.Items) > 0 || item.Extra > 0
		open, seen := a.assist.open.tree[id]
		if !seen {
			// The root row and its direct children are visible at once;
			// deeper branches wait for their parent's disclosure.
			open = depth == 0
		}
		row := ui.Row(keyed).FillWidth().MinWidth(0).Height(24).Padding(0, 6, 0, 14*float32(depth)).
			Gap(4).AlignItems(ui.Center)
		row.Children(func() {
			if hasKids {
				chev := ui.ButtonBase(c.Key("chevron")).Size(18, 18).Radius(4).
					Label("展开或折叠").Tooltip("展开或折叠")
				chev.Children(func() { chevronIcon(c, open, 12) })
				if chev.Clicked() {
					a.assist.open.tree[id] = !open
				}
			} else {
				ui.Box(c).Size(18, 18)
			}
			ui.Box(c.Key("icon")).Size(16, 16).Shrink(0).Draw(func(painter *ui.Painter, r ui.Rect) {
				drawFolder(painter, r, colorFolder())
			})
			label := ui.ButtonBase(c.Key("label")).FillWidth().MinWidth(0).Padding(0).
				Label(item.Text).Tooltip(item.Path)
			label.Children(func() {
				ui.Text(c, item.Text).FontSize(12).TextColor(colorText()).SingleLine().
					Ellipsis("…").MinWidth(0).Grow(1)
			})
			if item.Path != "" && label.Clicked() {
				a.assistOpenPath(item.Path)
			}
		})
		if open {
			a.paintOpenTreeItems(c, item, p, depth+1)
			if item.Extra > 0 {
				ui.Row(c.Key("extra")).FillWidth().Height(24).Padding(0, 6, 0, 14*float32(depth+1)).
					Children(func() {
						ui.Text(c, "…另有 "+itoa(item.Extra)+" 项").FontSize(12).TextColor(colorMuted())
					})
			}
		}
	}
	if parent.Extra > 0 && depth == 0 {
		ui.Row(c.Key("extra-root")).FillWidth().Height(24).Padding(0, 6, 0, 14*float32(depth+1)).
			Children(func() {
				ui.Text(c, "…另有 "+itoa(parent.Extra)+" 项").FontSize(12).TextColor(colorMuted())
			})
	}
}

func treeItemKey(card string, v *ouiView, depth, index int) string {
	if v == nil {
		return card + "\x00tree-item"
	}
	if v.Path != "" {
		return card + "\x00" + v.Path
	}
	return card + "\x00" + itoa(depth) + "\x00" + itoa(index) + "\x00" + v.Text
}

func (a *App) openValue(key, name string) *string {
	st := &a.assist.open
	if st.vals == nil {
		st.vals = map[string]*string{}
	}
	id := key + "/" + name
	if st.vals[id] == nil {
		st.vals[id] = new(string)
	}
	return st.vals[id]
}

func (a *App) paintOpenField(c *ui.Context, v *ouiView, p *openPaint) {
	// A presentation component inside a FormControl (ToOpenUI wraps every
	// scene field in one) is not a field: draw it as itself.
	if !openIsField(v.Kind) {
		a.paintOpenExtra(c, v, p, 0)
		return
	}
	label := v.Label
	if label == "" {
		label = v.Name
	}
	if v.Required && !strings.HasSuffix(label, " *") {
		label += " *"
	}
	val := a.openValue(p.key, v.Name)
	kind := v.Kind
	if kind == "input" {
		if v.Multiline {
			kind = "textarea"
		} else if v.Password {
			kind = "password"
		} else {
			kind = "text"
		}
	}
	if strings.TrimSpace(*val) == "" && v.Placeholder != "" {
		switch kind {
		case "progress", "slider", "tags", "time", "date", "number", "text", "textarea", "password",
			"image", "video":
			*val = v.Placeholder
		}
	}
	ui.Box(c.Key("field-" + v.Name)).FillWidth().MinWidth(0).Children(func() {
		a.paintFormField(c, kind, label, val, formFieldOpts{
			Locked:   p.locked,
			PathMode: v.PathMode,
			Options:  v.Options,
			Password: v.Password,
			Hint:     v.Placeholder,
			Min:      v.Min,
			Max:      v.Max,
		})
	})
}

func openDanger(variant string) bool {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "danger", "destructive":
		return true
	}
	return false
}

func (a *App) paintOpenButton(c *ui.Context, v *ouiView, p *openPaint) {
	variant := openButtonVariant(v.Variant)
	if p.locked && v.Label != p.chosen {
		variant = components.ButtonGhost
	}
	danger := openDanger(v.Variant)
	confirmID := p.key + "\x00" + v.Label
	st := &a.assist.open
	if danger && st.confirm == confirmID && !p.locked {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
			components.Icon(c, lucide.TriangleAlert()).Size(14, 14).TextColor(tokDanger)
			ui.Text(c, "确定要「"+openShort(v.Label, 24)+"」吗？").FontSize(12).TextColor(colorText())
			if components.Button(c, "确定", components.ButtonPrimary).Clicked() {
				st.confirm = ""
				a.openSend(v, p)
			}
			if components.Button(c, "取消", components.ButtonSecondary).Clicked() {
				st.confirm = ""
			}
		})
		return
	}
	form := p.form
	press := func() {
		if danger {
			st.confirm = confirmID
			return
		}
		saved := p.form
		p.form = form
		a.openSend(v, p)
		p.form = saved
	}
	btn := components.Button(c, openShort(v.Label, 48), variant)
	if p.locked || a.assist.running {
		btn.Disabled(true)
	} else {
		p.keys = append(p.keys, openShortcut{label: v.Label, primary: variant == components.ButtonPrimary && !danger, run: press})
		if btn.Clicked() {
			press()
		}
	}
}

// openSend posts the button's message, with the form's values under it
// when the button submits one. A required field left empty stops it.
func (a *App) openSend(v *ouiView, p *openPaint) {
	msg, missing := a.openMessage(v, p)
	if missing != "" {
		a.statusMsg = "请填写「" + missing + "」"
		return
	}
	a.assistantSendOpen(p.turn, msg, p.key, v.Label)
}

// openMessage is what a button sends: its action, then a line per filled
// field of the form it is in. missing names a required field left empty.
func (a *App) openMessage(v *ouiView, p *openPaint) (msg, missing string) {
	msg = v.Action
	if p.form == nil {
		return msg, ""
	}
	var lines []string
	for _, f := range p.form.Children {
		if !openIsField(f.Kind) {
			continue
		}
		label := f.Label
		if label == "" {
			label = f.Name
		}
		val := strings.TrimSpace(*a.openValue(p.key, f.Name))
		if val == "" && f.Required {
			return "", label
		}
		if val != "" {
			lines = append(lines, label+"："+val)
		}
	}
	if len(lines) > 0 {
		msg += "\n" + strings.Join(lines, "\n")
	}
	return msg, ""
}

// paintOpenTable is a Table: a path in a cell opens it in this window, and
// a long table scrolls inside the card instead of stretching the reply.
func (a *App) paintOpenTable(c *ui.Context, v *ouiView) {
	cols := make([]ui.TableColumn, len(v.Cols))
	for i, col := range v.Cols {
		cols[i] = ui.TableColumn{Title: col.Label, ID: itoa(i), MinWidth: 56, MaxWidth: 220}
	}
	n := 0
	for _, col := range v.Cols {
		n = max(n, len(col.Cells))
	}
	cell := func(r, ci int) string {
		if ci >= 0 && ci < len(v.Cols) && r < len(v.Cols[ci].Cells) {
			return v.Cols[ci].Cells[r]
		}
		return ""
	}
	table := func() {
		components.Table(c, nil, cols, func() {
			for r := 0; r < n; r++ {
				components.TableRow(c, r, cell(r, 0), false, func(ci int) {
					text := cell(r, ci)
					shown := openCellText(text)
					if p, ok := linkPath(text); ok && text != "" {
						link := ui.ButtonBase(c.Key(ci)).MinWidth(0).Label("打开 " + text).Tooltip(text)
						link.Children(func() {
							t := ui.Text(c, shown).FontSize(12).TextColor(tokAccent).SingleLine().Tooltip(text)
							if link.Hovered() {
								t.Underline()
							}
						})
						if link.Clicked() {
							a.assistOpenPath(p)
						}
						return
					}
					ui.Text(c.Key(ci), shown).FontSize(12).TextColor(colorText()).SingleLine().Tooltip(text)
				})
			}
		})
	}
	if n > 8 {
		ui.Scroll(c).FillWidth().MaxHeight(300).Children(table)
		return
	}
	table()
}

// openCellText keeps table cells one line and bounded. Path cells use the
// search breadcrumb's middle elision so their root and nearest folders stay
// identifiable; every other cell keeps its head and tail around one ellipsis.
func openCellText(text string) string {
	_, isPath := linkPath(text)
	if isPath {
		text = searchPathText(searchPathSegments(text, homeForSearch(), 5))
		return middleEllipsisText(text, 44)
	}
	return middleEllipsisText(text, 32)
}

// middleEllipsisText collapses the middle of a long label while preserving
// enough at both ends to identify it in a narrow table cell.
func middleEllipsisText(text string, max int) string {
	r := []rune(text)
	if max < 3 || len(r) <= max {
		return text
	}
	head := (max - 1) / 2
	tail := max - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// assistantSendOpen posts a card's message as the next user message and
// locks the card on the label that sent it. The click does not touch files;
// the following turn does, under the same confirmation rules as anything
// typed into the composer.
func (a *App) assistantSendOpen(i int, action, key, label string) {
	action = strings.TrimSpace(limitRunes(action, 2000))
	if action == "" || a.assist.running {
		return
	}
	if i >= 0 && i < len(a.assist.turns) && a.assist.turns[i].OpenDone[key] != "" {
		return
	}
	before := len(a.assist.turns)
	a.assist.draft = action
	a.assistantSubmit()
	if len(a.assist.turns) == before || i < 0 || i >= before {
		return
	}
	t := &a.assist.turns[i]
	t.OpenSent = append(t.OpenSent, action)
	if t.OpenDone == nil {
		t.OpenDone = map[string]string{}
	}
	t.OpenDone[key] = label
	a.assist.persistSession(a.assist.folder)
}
