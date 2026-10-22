package main

// assistant_slash.go is the “/” command menu above the assistant composer.
// GlassComposer already sends on Enter and inserts a newline on Shift+Enter;
// this file only opens a filtered popup while the draft is a single-line
// slash query, and intercepts ↑↓ Enter Esc so choosing a row fills text or
// runs an action without rebinding those keys for ordinary messages.

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	assistant "sunvale/internal/assistant"
)

// assistSlashKind is what selecting a row does.
type assistSlashKind int

const (
	slashFill assistSlashKind = iota
	slashAction
)

// assistSlashItem is one row in the slash popup.
type assistSlashItem struct {
	ID, Trigger, Label, Detail, Fill string
	Kind                             assistSlashKind
	Run                              func(*App)
}

type slashEnterKey struct{}

// assistSlashDraft reports whether the draft is a single-line “/…” query.
func assistSlashDraft(draft string) bool {
	if !strings.HasPrefix(draft, "/") {
		return false
	}
	return !strings.Contains(draft, "\n")
}

func assistSlashQuery(draft string) string {
	if !assistSlashDraft(draft) {
		return ""
	}
	return draft[len("/"):]
}

func (a *App) assistSlashCatalog() []assistSlashItem {
	items := []assistSlashItem{
		{
			ID: "mem", Trigger: "/记忆", Label: "记忆", Detail: "打开会话与记忆",
			Kind: slashAction,
			Run: func(app *App) {
				app.assist.showHist = true
				app.assist.draft = "记住："
			},
		},
		{
			ID: "organize", Trigger: "/整理", Label: "整理", Detail: "填入整理提示，不发送",
			Kind: slashFill, Fill: "整理当前文件夹，把同类文件归到一起",
		},
		{
			ID: "rename", Trigger: "/重命名", Label: "重命名", Detail: "填入重命名提示，不发送",
			Kind: slashFill, Fill: "按拍摄日期重命名照片",
		},
		{
			ID: "classify", Trigger: "/分类", Label: "分类", Detail: "填入按内容分类下载的提示",
			Kind: slashFill, Fill: assistSlashClassifyFill(),
		},
		{
			ID: "run-now", Trigger: "/立即执行", Label: "立即执行", Detail: "运行最近的定时任务，或打开定时任务页",
			Kind: slashAction,
			Run: func(app *App) {
				app.assist.draft = ""
				app.assistSlashRunLastSchedule()
			},
		},
		{
			ID: "clear", Trigger: "/清空会话", Label: "清空会话", Detail: "结束当前对话并开新会话",
			Kind: slashAction,
			Run: func(app *App) {
				app.assist.draft = ""
				app.assistantNewSession()
			},
		},
		{
			ID: "new", Trigger: "/新会话", Label: "新会话", Detail: "结束当前对话并开新会话",
			Kind: slashAction,
			Run: func(app *App) {
				app.assist.draft = ""
				app.assistantNewSession()
			},
		},
		{
			ID: "settings", Trigger: "/设置", Label: "设置", Detail: "打开助手设置",
			Kind: slashAction,
			Run: func(app *App) {
				app.assist.draft = ""
				app.openAssistantSettings()
			},
		},
	}
	prefs := assistPrefs{DisabledSkills: a.assist.disabledSkills}
	if !assistant.SkillDisabled(prefs, assistant.SkillFiles) {
		items = append(items, assistSlashItem{
			ID: "skill-files", Trigger: "/文件能力", Label: "文件能力", Detail: "填写文件能力提示",
			Kind: slashFill, Fill: "用文件能力处理当前文件夹里的文件",
		})
	}
	if !assistant.SkillDisabled(prefs, assistant.SkillMemory) {
		items = append(items, assistSlashItem{
			ID: "skill-memory", Trigger: "/记忆能力", Label: "记忆能力", Detail: "填写记忆能力提示",
			Kind: slashFill, Fill: "把下面这件事记下来：",
		})
	}
	if !assistant.SkillDisabled(prefs, assistant.SkillArchive) {
		items = append(items, assistSlashItem{
			ID: "skill-archive", Trigger: "/压缩能力", Label: "压缩能力", Detail: "填写压缩与解压提示",
			Kind: slashFill, Fill: "帮我压缩或解压选中的文件",
		})
	}
	return items
}

func assistSlashFilter(items []assistSlashItem, query string) []assistSlashItem {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return items
	}
	var out []assistSlashItem
	for _, it := range items {
		hay := strings.ToLower(it.Trigger + " " + it.Label + " " + it.Detail)
		if strings.Contains(hay, q) || strings.HasPrefix(strings.ToLower(strings.TrimPrefix(it.Trigger, "/")), q) {
			out = append(out, it)
		}
	}
	return out
}

func (a *App) assistSlashOpen() bool {
	if !assistSlashDraft(a.assist.draft) {
		a.assist.slashHide = false
		return false
	}
	return !a.assist.slashHide
}

func (a *App) assistSlashItems() []assistSlashItem {
	return assistSlashFilter(a.assistSlashCatalog(), assistSlashQuery(a.assist.draft))
}

func (a *App) assistSlashClamp(n int) {
	if n <= 0 {
		a.assist.slashIdx = 0
		return
	}
	if a.assist.slashIdx < 0 {
		a.assist.slashIdx = 0
	}
	if a.assist.slashIdx >= n {
		a.assist.slashIdx = n - 1
	}
}

func (a *App) assistSlashApply(it assistSlashItem) {
	a.assist.slashHide = false
	a.assist.slashIdx = 0
	switch it.Kind {
	case slashFill:
		a.assist.draft = it.Fill
	case slashAction:
		if it.Run != nil {
			it.Run(a)
		}
	}
}

func (a *App) assistantSlashMenu(c *ui.Context, items []assistSlashItem) {
	a.assistSlashClamp(len(items))
	panel := ui.Column(c.Key("assist-slash")).FillWidth().MaxHeight(220).
		Padding(4).Radius(8).Background(colorAddress()).Border(1, colorDivider()).
		Shadow(0, 8, 16, 0, ui.RGBA(0, 0, 0, 0.14)).Label("助手命令")
	panel.Children(func() {
		if len(items) == 0 {
			ui.Row(c).FillWidth().Height(28).Padding(0, 8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "无匹配命令").FontSize(12).TextColor(colorMuted())
			})
			return
		}
		ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			for i, it := range items {
				a.assistantSlashRow(c, i, it)
			}
		})
	})
}

func (a *App) assistantSlashRow(c *ui.Context, i int, it assistSlashItem) {
	row := ui.Row(c.Key("slash:"+it.ID)).FillWidth().Height(28).Padding(0, 8).Gap(8).
		Radius(4).AlignItems(ui.Center).Shrink(0).Label(it.Trigger)
	switch {
	case i == a.assist.slashIdx:
		row.Background(colorSelected())
		row.ScrollIntoView()
	case row.Pressed():
		row.Background(colorPressed())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		components.Icon(c, lucide.CornerDownLeft).Size(14, 14).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, it.Trigger).FontSize(12).TextColor(colorText()).SingleLine().Shrink(0)
		if it.Detail != "" {
			ui.Text(c, it.Detail).FontSize(11).TextColor(colorMuted()).SingleLine().Grow(1).MinWidth(0)
		}
	})
	if row.Clicked() {
		a.assistSlashApply(it)
	}
}

// assistantSlashKeys steals ↑↓ Enter Esc while the menu is open. Calling
// HandleInput replaces GlassComposer's Enter-to-send hook for this frame only;
// ordinary drafts keep the library behaviour.
func (a *App) assistantSlashKeys(input ui.Element, items []assistSlashItem) {
	pending := ui.Local(input, slashEnterKey{}, func() int { return 0 })
	input.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputKeyDown || ev.Mods != 0 {
			return false
		}
		switch ev.Key {
		case ui.KeyEscape:
			a.assist.slashHide = true
			a.assist.slashIdx = 0
			return true
		case ui.KeyDown:
			if len(items) == 0 {
				return true
			}
			a.assist.slashIdx = (a.assist.slashIdx + 1) % len(items)
			return true
		case ui.KeyUp:
			if len(items) == 0 {
				return true
			}
			if a.assist.slashIdx <= 0 {
				a.assist.slashIdx = len(items) - 1
			} else {
				a.assist.slashIdx--
			}
			return true
		case ui.KeyEnter:
			if len(items) == 0 {
				return true
			}
			*pending = a.assist.slashIdx + 1
			return true
		}
		return false
	})
	if *pending > 0 {
		i := *pending - 1
		*pending = 0
		if i >= 0 && i < len(items) {
			a.assistSlashApply(items[i])
		}
	}
}

func (a *App) assistantComposerTrailing(c *ui.Context) {
	// GlassComposer shows Stop only when Running and the draft is empty; a
	// slash draft would hide it. Put Stop back in Trailing so the user can
	// still cancel while typing.
	if !a.assist.running || strings.TrimSpace(a.assist.draft) == "" {
		return
	}
	if chat.IconAction(c, "停止", lucide.Square).Clicked() && a.assist.stop != nil {
		a.assist.queued = ""
		a.assist.stop()
		a.assist.status = "正在停止…"
	}
}

func assistSlashClassifyFill() string {
	return "现在把下载文件夹按内容分类到文档、图片、安装包、其他（用助手任务，不要只按扩展名移动）"
}

// assistSlashRunLastSchedule runs the most recently run job, else the first
// job in the store; with no jobs it opens Settings → 定时任务.
func (a *App) assistSlashRunLastSchedule() {
	st := ensureScheduleStore()
	jobs := st.Jobs()
	if len(jobs) == 0 {
		a.openScheduleSettings()
		a.statusMsg = "暂无定时任务，已打开设置"
		return
	}
	best := jobs[0]
	for _, j := range jobs[1:] {
		if j.LastRunAt.After(best.LastRunAt) {
			best = j
		}
	}
	runScheduledJobNow(best.ID)
	a.statusMsg = "已开始运行：" + best.Name
}
