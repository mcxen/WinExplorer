package main

import (
	"path/filepath"
	"sort"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/internal/tasks"
)

// tasks_view.go is the back of the middle area: every task as a card —
// title, source, state, the bar or gauge, the numbers, the member in hand
// with its own bar, the recent and failed members on demand, and the
// controls (TASKSYS T4).

var taskTabs = []string{"进行中", "排队", "失败", "历史"}

func taskTabOf(s archiveTaskSnap) int {
	switch s.State {
	case tasks.Running, tasks.Paused:
		return 0
	case tasks.Queued:
		return 1
	case tasks.Failed, tasks.Partial:
		return 2
	}
	return 3
}

// taskViewLists splits this session's tasks and the persisted history
// into the four tabs; history is newest first and holds every finished
// task, failed ones included.
func taskViewLists() [4][]archiveTaskSnap {
	var out [4][]archiveTaskSnap
	seen := map[string]bool{}
	session := archiveSnaps(nil)
	for _, s := range session {
		seen[s.ID] = true
		if tab := taskTabOf(s); tab < 3 {
			out[tab] = append(out[tab], s)
		}
	}
	var hist []archiveTaskSnap
	for i := len(session) - 1; i >= 0; i-- {
		if session[i].Finished {
			hist = append(hist, session[i])
		}
	}
	for _, h := range taskManager().History() {
		if !seen[h.ID] {
			hist = append(hist, snapFromTask(h))
		}
	}
	sort.SliceStable(hist, func(i, j int) bool { return hist[i].Start.After(hist[j].Start) })
	out[3] = hist
	return out
}

func taskSourceLabel(src string) string {
	switch tasks.Source(src) {
	case tasks.SourceAI:
		return "AI"
	case tasks.SourceSchedule:
		return "定时"
	case tasks.SourceRemote:
		return "远程"
	}
	return "我"
}

func taskStateLabel(s archiveTaskSnap) (string, ui.Color) {
	if s.Ask != nil {
		return "待选择", tokWarning
	}
	switch s.State {
	case tasks.Running:
		return "进行中", colorAccent()
	case tasks.Paused:
		return "已暂停", tokWarning
	case tasks.Queued:
		return "排队", colorMuted()
	case tasks.Done:
		return "完成", tokSuccess
	case tasks.Partial:
		return "部分失败", tokDanger
	case tasks.Failed:
		return "失败", tokDanger
	}
	return "已取消", colorMuted()
}

func (a *App) tasksView(c *ui.Context) {
	var lists [4][]archiveTaskSnap
	limit := taskListPage
	if f := a.flip; f != nil {
		// Mid-flip: the lists gathered when the flip began, and only the
		// cards that fit, so the turning face builds the same light tree
		// each frame.
		if f.lists == nil {
			l := taskViewLists()
			f.lists = &l
		}
		lists, limit = *f.lists, flipCards
	} else {
		lists = taskViewLists()
	}
	if a.tasksTab < 0 || a.tasksTab > 3 {
		a.tasksTab = 0
	}
	ui.Column(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Background(colorContent()).Label("任务").Children(func() {
		ui.Row(c.Key("head")).FillWidth().Height(44).Padding(0, 14).Gap(10).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(colorDivider()).Children(func() {
			labels := make([]string, 4)
			for i, t := range taskTabs {
				labels[i] = t + " " + itoa(len(lists[i]))
			}
			ui.Segmented(c, &a.tasksTab, labels...).Shrink(0)
			ui.Box(c).Grow(1)
			var speed float64
			paused := 0
			for _, s := range lists[0] {
				if s.State == tasks.Running {
					speed += s.Speed
				} else {
					paused++
				}
			}
			if speed > 0 {
				ui.Text(c.Key("speed"), formatBytes(int64(speed))+"/s").FontSize(12.5).TextColor(colorMuted()).Shrink(0)
			}
			m := taskManager()
			if len(lists[0]) > 0 {
				if paused == len(lists[0]) {
					if components.IconButton(c.Key("resume-all"), lucide.Play(), "全部继续", components.IconButtonPlain).Clicked() {
						m.ResumeAll()
					}
				} else if components.IconButton(c.Key("pause-all"), lucide.Pause(), "全部暂停", components.IconButtonPlain).Clicked() {
					m.PauseAll()
				}
			}
			if components.IconButton(c.Key("clear"), lucide.ListX(), "清除已完成", components.IconButtonPlain).Clicked() {
				m.ClearFinished()
			}
			if components.IconButton(c.Key("back"), lucide.Undo2(), "返回文件 (Esc)", components.IconButtonPlain).Clicked() {
				a.tasksFlipped = false
			}
		})
		gauge := a.assistProgressStyle() == progressStyleGauge
		ui.Scroll(c.Key("list")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("cards")).FillWidth().Padding(12, 14).Gap(10).Children(func() {
				list := lists[a.tasksTab]
				if a.tasksOpen["all"] && a.flip == nil {
					limit = len(list)
				}
				for i, s := range list {
					if i >= limit {
						break
					}
					a.taskCard(c.Key("card-"+s.ID), s, gauge)
				}
				if n := len(list) - limit; n > 0 && a.flip == nil {
					if components.Button(c.Key("more"), "显示其余 "+itoa(n)+" 项", components.ButtonGhost).Clicked() {
						if a.tasksOpen == nil {
							a.tasksOpen = map[string]bool{}
						}
						a.tasksOpen["all"] = true
					}
				}
			})
		})
	})
	if len(lists[0]) > 0 {
		c.After(archiveRepaint)
	}
}

func (a *App) taskCard(c *ui.Context, s archiveTaskSnap, gauge bool) {
	tone := snapTone(s)
	if s.State == tasks.Paused {
		tone = toneCanceled
	}
	if s.State == tasks.Partial {
		tone = toneError
	}
	_, icon := archiveSnapTone(s)
	cardR := float32(10)
	if visualV2 {
		cardR = v2CardRadius
	}
	card := ui.Row(c).FillWidth().MinWidth(0).Gap(14).Padding(12, 14).Radius(cardR).AlignItems(ui.Start)
	if visualV2 {
		v2Raised(card.Background(colorSurface()))
	} else {
		card.Border(1, colorDivider()).Background(colorSurface())
	}
	card.Children(func() {
		if gauge {
			paintGauge(c, "tv-gauge-"+s.ID, s.Frac(), tone)
		}
		ui.Column(c.Key("body")).Grow(1).MinWidth(0).Gap(6).Children(func() {
			a.taskCardHead(c, s, icon, tone)
			if !gauge {
				frac := s.Frac()
				if s.State == tasks.Queued {
					frac = 0 // waiting, not working: no sweep
				}
				paintProgressBar(c, "tv-bar-"+s.ID, frac, tone, 10)
			}
			ui.Text(c.Key("nums"), taskNumbers(s)).FontSize(12.5).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0)
			if s.Cur != "" {
				ui.Row(c.Key("cur")).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(16, 16).Shrink(0).Center().Children(func() {
						a.fileIcon(c, Entry{Name: filepath.Base(s.Cur), Ext: lowerExt(s.Cur)}, 16)
					})
					name := ui.Text(c.Key("n"), s.Cur).FontSize(12.5).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
					components.Tooltip(c, name, s.Cur)
					ui.Box(c.Key("b")).Width(160).Height(5).Shrink(0).AlignSelf(ui.Center).Children(func() {
						paintProgressBar(c, "tv-cur-"+s.ID, s.CurFrac(), toneRunning, 5)
					})
					sz := formatBytes(s.CurDone)
					if s.CurTotal > 0 {
						sz += " / " + formatBytes(s.CurTotal)
					}
					ui.Text(c.Key("s"), sz).FontSize(12).TextColor(colorMuted()).Shrink(0).Width(130)
				})
			}
			if s.Ask != nil {
				a.taskAsk(c, s)
			}
			if s.Err != "" && s.State != tasks.Canceled && s.State != tasks.Partial {
				ui.Text(c.Key("err"), s.Err).FontSize(12.5).TextColor(tokDanger).Wrap().Selectable()
			}
			a.taskCardLists(c, s)
		})
	})
}

func (a *App) taskCardHead(c *ui.Context, s archiveTaskSnap, icon *ui.SVG, tone progressTone) {
	ui.Row(c.Key("head")).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(18, 18).Shrink(0).Center().Children(func() {
			components.Icon(c, icon).Size(18, 18).TextColor(tone.color(false))
		})
		title := s.Title
		if title == "" {
			title = archiveVerb(s.Kind) + " " + s.Name
		}
		t := ui.Text(c.Key("t"), title).FontSize(14).FontWeight(600).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
		components.Tooltip(c, t, s.Path)
		ui.Text(c.Key("src"), taskSourceLabel(s.Origin)).FontSize(11).TextColor(colorMuted()).
			Padding(1, 6).Radius(4).Background(colorAddress()).Shrink(0)
		label, col := taskStateLabel(s)
		ui.Text(c.Key("st"), label).FontSize(11.5).FontWeight(600).TextColor(col).Shrink(0)
		m := taskManager()
		btn := func(key string, ic *ui.SVG, tip string) bool {
			return components.IconButton(c.Key(key), ic, tip, components.IconButtonPlain).Size(28, 28).Clicked()
		}
		switch s.State {
		case tasks.Running:
			if btn("pause", lucide.Pause(), "暂停") {
				_ = m.Pause(s.ID)
			}
		case tasks.Paused:
			if btn("resume", lucide.Play(), "继续") {
				_ = m.Resume(s.ID)
			}
		}
		if !s.Finished {
			if btn("cancel", lucide.X(), "取消") {
				_ = m.Cancel(s.ID)
			}
		} else if s.State != tasks.Done && s.Kind != string(tasks.KindScript) {
			if btn("retry", lucide.RotateCcw(), "重试") {
				a.retryTask(s)
			}
		}
		if s.Path != "" {
			if btn("open", lucide.FolderOpen(), "打开位置") {
				a.openTaskLocation(s.Path)
			}
		}
	})
}

// taskNumbers is the dense line: percent, bytes, speed, ETA, items.
// taskAsk is a name clash asked inline: the item, three choices and
// whether to apply the choice to the rest of the run.
func (a *App) taskAsk(c *ui.Context, s archiveTaskSnap) {
	q := s.Ask
	ui.Row(c.Key("ask")).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Padding(6, 8).Radius(6).
		Background(tokWarning.Alpha(0.12)).Children(func() {
		components.Icon(c, lucide.TriangleAlert()).Size(16, 16).TextColor(tokWarning).Shrink(0)
		name := ui.Text(c.Key("n"), q.Item).FontSize(12.5).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
		components.Tooltip(c, name, q.Item+" "+q.Detail)
		ui.Text(c.Key("d"), q.Detail).FontSize(12).TextColor(colorMuted()).Shrink(0)
		all := a.tasksOpen[s.ID+"all"]
		if q.Rest > 0 {
			components.Checkbox(c.Key("all"), &all, "其余 "+itoa(q.Rest)+" 项相同")
			if a.tasksOpen == nil {
				a.tasksOpen = map[string]bool{}
			}
			a.tasksOpen[s.ID+"all"] = all
		}
		labels := map[string]string{tasks.ChoiceReplace: "替换", tasks.ChoiceSkip: "跳过", tasks.ChoiceKeep: "保留两者"}
		for i, opt := range q.Options {
			style := components.ButtonSecondary
			if i == len(q.Options)-1 {
				style = components.ButtonPrimary
			}
			if components.Button(c.Key("o"+opt), labels[opt], style).Clicked() {
				_ = taskManager().Answer(s.ID, tasks.Answer{Choice: opt, All: all})
			}
		}
	})
}

func taskNumbers(s archiveTaskSnap) string {
	if s.State == tasks.Queued {
		return "等待磁盘空位（每个磁盘同时 2 个）"
	}
	out := archiveMeta(s)
	if s.ItemsTotal > 0 {
		out += " · " + itoa(s.ItemsDone) + " / " + itoa(s.ItemsTotal) + " 项"
	} else if s.ItemsDone > 0 {
		out += " · " + itoa(s.ItemsDone) + " 项"
	}
	return out
}

// taskCardLists is the expandable recent and failed members.
func (a *App) taskCardLists(c *ui.Context, s archiveTaskSnap) {
	if len(s.Recent) == 0 && s.FailedCount == 0 {
		return
	}
	if a.tasksOpen == nil {
		a.tasksOpen = map[string]bool{}
	}
	toggle := func(key, label string) {
		open := a.tasksOpen[s.ID+key]
		ic := lucide.ChevronRight()
		if open {
			ic = lucide.ChevronDown()
		}
		b := ui.ButtonBase(c.Key("tg-"+key)).Radius(4).Padding(2, 4).Label(label)
		b.Children(func() {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				components.Icon(c, ic).Size(12, 12).TextColor(colorMuted())
				col := colorMuted()
				if key == "f" {
					col = tokDanger
				}
				ui.Text(c, label).FontSize(12).TextColor(col)
			})
		})
		if b.Clicked() {
			a.tasksOpen[s.ID+key] = !open
		}
	}
	ui.Row(c.Key("toggles")).Gap(10).Children(func() {
		if len(s.Recent) > 0 {
			toggle("r", "最近 "+itoa(len(s.Recent))+" 项")
		}
		if s.FailedCount > 0 {
			toggle("f", "失败 "+itoa(s.FailedCount)+" 项")
		}
	})
	if a.tasksOpen[s.ID+"r"] {
		ui.Column(c.Key("recent")).FillWidth().Gap(2).Padding(0, 0, 0, 20).Children(func() {
			for i := len(s.Recent) - 1; i >= 0; i-- {
				it := s.Recent[i]
				ui.Row(c.Key("ri" + itoa(i))).FillWidth().MinWidth(0).Gap(8).Children(func() {
					ui.Text(c.Key("n"), it.Name).FontSize(12).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
					if it.Size > 0 {
						ui.Text(c.Key("s"), formatBytes(it.Size)).FontSize(12).TextColor(colorMuted()).Shrink(0)
					}
				})
			}
		})
	}
	if a.tasksOpen[s.ID+"f"] {
		ui.Column(c.Key("failed")).FillWidth().Gap(2).Padding(0, 0, 0, 20).Children(func() {
			for i, it := range s.Failed {
				ui.Row(c.Key("fi" + itoa(i))).FillWidth().MinWidth(0).Gap(8).Children(func() {
					ui.Text(c.Key("n"), it.Name).FontSize(12).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
					ui.Text(c.Key("e"), it.Err).FontSize(12).TextColor(tokDanger).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				})
			}
		})
	}
}

// openTaskLocation shows the task's path in its folder, front side up.
func (a *App) openTaskLocation(path string) {
	a.tasksFlipped = false
	a.navigate(filepath.Dir(path))
	a.selectOnly(path)
}

// retryTask reruns a task from its stored inputs; a task from an earlier
// session has none, so an extract falls back to unpacking the path again.
func (a *App) retryTask(s archiveTaskSnap) {
	if _, err := taskManager().Retry(s.ID); err == nil {
		return
	}
	if s.Kind == string(tasks.KindExtract) && s.Path != "" {
		a.retryExtract(s.Path)
		return
	}
	a.statusMsg = "无法重试：任务来自上次运行"
}

// retryExtract unpacks the archive again, as 解压 on it would.
func (a *App) retryExtract(path string) {
	a.assist.ensure()
	e := Entry{Name: filepath.Base(path), Path: path}
	candidates := archivePasswordCandidates(a.assist.passwords)
	aiFx.markBusy(path)
	archiveGo(func() { a.extractArchives(filepath.Dir(path), []Entry{e}, candidates) })
}
