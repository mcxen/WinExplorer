package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
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
	// keys are the Alt+number shortcuts of the latest card still open,
	// gathered as the transcript builds.
	keys []openShortcut
}

type openShortcut struct {
	label   string
	primary bool
	run     func()
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
			Background(colorHover()).Children(func() {
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
	case "form":
		outer := p.form
		p.form = v
		ui.Column(c).FillWidth().MinWidth(0).Gap(8).Children(func() {
			for i, ch := range v.Children {
				a.paintOpenView(c.Key(i), ch, p, depth+1)
			}
		})
		p.form = outer
	case "input", "select":
		a.paintOpenField(c, v, p)
	case "button":
		a.paintOpenButton(c, v, p)
	}
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
	label := v.Label
	if label == "" {
		label = v.Name
	}
	val := a.openValue(p.key, v.Name)
	ui.Column(c.Key("field-" + v.Name)).FillWidth().MinWidth(0).Gap(4).Children(func() {
		title := label
		if v.Required {
			title += " *"
		}
		ui.Text(c, title).FontSize(12).TextColor(colorText())
		if v.Kind == "select" {
			if *val == "" {
				*val = v.Options[0]
			}
			sel := ui.Select(c, val, v.Options).FillWidth()
			if p.locked {
				sel.Disabled(true)
			}
			return
		}
		box := components.TextBox(c, val, components.TextBoxOptions{Multiline: v.Multiline, ReadOnly: p.locked}).
			FillWidth().Label(label)
		if v.Placeholder != "" {
			box.Placeholder(v.Placeholder)
		}
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
		cols[i] = ui.TableColumn{Title: col.Label, ID: itoa(i), MinWidth: 56}
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
					if p, ok := linkPath(text); ok && text != "" {
						link := ui.ButtonBase(c.Key(ci)).MinWidth(0).Label("打开 " + text).Tooltip("在窗口中打开")
						link.Children(func() {
							t := ui.Text(c, text).FontSize(12).TextColor(tokAccent).SingleLine()
							if link.Hovered() {
								t.Underline()
							}
						})
						if link.Clicked() {
							a.assistOpenPath(p)
						}
						return
					}
					ui.Text(c.Key(ci), text).FontSize(12).TextColor(colorText()).SingleLine()
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
