package main

import (
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistant_hist.go is the docked history column (CHATHIST variant B,
// docs/specs/13-chat-history.md): one 28px row per session, pinned first,
// then recency groups; hover reveals pin/rename/delete.

const (
	assistHistW    = 176
	assistHistMinW = 140
	assistHistMaxW = 320
)

func (u *assistantUI) histWidth() float32 {
	if u.histW < assistHistMinW {
		return assistHistW
	}
	return min(u.histW, assistHistMaxW)
}

// assistantHistGrip drags the column's right edge; saved on release.
func (a *App) assistantHistGrip(c *ui.Context) {
	grip := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-3).Width(7).Cursor(ui.CursorResizeEW)
	a.assist.histDragging = grip.Dragging()
	if dx, _, ok := grip.Dragged(); ok && dx != 0 {
		a.assist.histW = max(assistHistMinW, min(assistHistMaxW, a.assist.histWidth()+dx))
		a.assist.histDirty = true
	}
	if grip.DoubleClicked() {
		a.assist.histW = assistHistW
		a.assist.histDirty = true
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		if grip.Hovered() || grip.Dragging() {
			p.Fill(ui.Rect{X: r.X + (r.W-1)/2, Y: r.Y, W: 1, H: r.H}, colorAccent(), 0)
		}
	})
}

// histSessions filters by the search box and orders pinned first, each
// part newest first.
func histSessions(list []aiSessionMeta, q string) []aiSessionMeta {
	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]aiSessionMeta, 0, len(list))
	for _, s := range list {
		if q == "" || strings.Contains(strings.ToLower(s.Title+" "+s.Preview+" "+s.Folder), q) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].Updated.After(out[j].Updated)
	})
	return out
}

// histTime is the row's right column: 15:04 today, 昨天, else 1月2日.
func histTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch assistTimelineGroup(t) {
	case "今天":
		return t.Format("15:04")
	case "昨天":
		return "昨天"
	}
	if t.Year() != time.Now().Year() {
		return t.Format("2006/1/2")
	}
	return t.Format("1月2日")
}

func (a *App) assistantRecords(c *ui.Context) {
	ui.Column(c).Fill().MinHeight(0).Padding(6).Gap(1).Children(func() {
		ui.Row(c.Key("hist-top")).FillWidth().Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
			components.TextBox(c.Key("hist-q"), &a.assist.histQuery, components.TextBoxOptions{LeadingIcon: lucide.Search()}).
				Grow(1).MinWidth(0).Height(26).FontSize(11.5).Placeholder("搜索").Label("搜索会话")
			if components.IconButton(c, lucide.PanelLeftClose(), "收起历史会话", components.IconButtonPlain).Width(26).Height(26).Clicked() {
				a.assist.showHist = false
				a.assist.histInline = false
			}
		})
		metas := histSessions(a.assist.sessions, a.assist.histQuery)
		ui.Scroll(c.Key("hist-list")).FillWidth().Grow(1).MinHeight(0).Gap(1).Children(func() {
			group := ""
			for _, meta := range metas {
				g := assistTimelineGroup(meta.Updated)
				if meta.Pinned {
					g = "置顶"
				}
				if g != group {
					group = g
					ui.Text(c, g).Key("hist-group-"+g).FontSize(10.5).FontWeight(600).
						TextColor(colorMuted()).Padding(8, 8, 2, 8)
				}
				a.assistantSessionRow(c, meta)
			}
		})
	})
}

func (a *App) assistantSessionRow(c *ui.Context, meta aiSessionMeta) {
	cc := c.Key(meta.ID)
	current := meta.ID == a.assist.sessionID
	if a.assist.renamingID == meta.ID {
		in := ui.TextInputBase(cc.Key("rename"), &a.assist.renameDraft).FillWidth().Height(28).
			FontSize(12).Padding(0, 8).Radius(4).Background(colorAddress()).Label("重命名会话")
		in.Focus()
		in.OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyEnter:
				a.assist.renameSession(meta.ID, a.assist.renameDraft)
				a.assist.renamingID = ""
				return true
			case ui.KeyEscape:
				a.assist.renamingID = ""
				return true
			}
			return false
		})
		return
	}
	label := meta.Title
	if label == "" {
		label = "新会话"
	}
	row := ui.ButtonBase(cc).FillWidth().Height(28).Padding(0, 8).Gap(6).AlignItems(ui.Center).Radius(4).Label(label)
	hover := row.Hovered()
	switch {
	case current:
		row.Background(colorSelected())
	case hover:
		row.Background(colorHover())
	}
	row.Children(func() {
		if meta.Pinned && !hover {
			components.Icon(cc, lucide.Pin()).Size(12, 12).TextColor(colorAccent()).Shrink(0)
		}
		t := ui.Text(cc.Key("t"), label).FontSize(12).TextColor(colorText()).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if current {
			t.FontWeight(600)
		}
		if !hover {
			ui.Text(cc.Key("w"), histTime(meta.Updated)).FontSize(11).TextColor(colorMuted()).Shrink(0)
			return
		}
		pin := "置顶"
		if meta.Pinned {
			pin = "取消置顶"
		}
		act := func(k string, ic *ui.SVG, name string) bool {
			return components.IconButton(cc.Key(k), ic, name, components.IconButtonPlain).Width(20).Height(20).Clicked()
		}
		if act("pin", lucide.Pin(), pin) {
			a.assist.pinSession(meta.ID, !meta.Pinned)
		}
		if act("ren", lucide.PencilLine(), "重命名") {
			a.assist.renamingID, a.assist.renameDraft = meta.ID, label
		}
		if act("del", lucide.Trash(), "删除会话") {
			a.assistantDeleteSession(meta.ID)
		}
	})
	if row.Clicked() {
		a.assistantOpenSession(meta.ID)
	}
}
