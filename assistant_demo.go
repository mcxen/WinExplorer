package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sunvale/internal/assistant"
	"sunvale/internal/fsops"
)

// assistant_demo.go is the assistant's offline demo. With
// SUNVALE_ASSIST_DEMO=1 a message is answered by a scripted turn instead of
// a model: the script picks a scenario by keyword (新建、修改、重命名/归类、
// 删除、批量、总结), works on real files in a throwaway sandbox folder, and
// drives the same cards, plan list and row effects a model run does. It
// never touches the folder the user had open: the first demo message seeds
// a temp folder and navigates there.

const assistDemoEnv = "SUNVALE_ASSIST_DEMO"

// assistDemoForce turns the demo on without the environment (tests).
var assistDemoForce bool

// assistDemoSync plays a script's beats inline instead of on a timer, so a
// test can stop between beats and look at the window.
var assistDemoSync bool

func assistDemoOn() bool { return assistDemoForce || os.Getenv(assistDemoEnv) == "1" }

// demoBeat is one step of a script. A gate beat stops the script until the
// user answers its card; the rest of the script waits in demoAfter.
type demoBeat struct {
	wait time.Duration
	do   func(a *App)
	gate string
}

// assistDemoSeed fills dir with the sandbox's sample files.
func assistDemoSeed(dir string) error {
	files := map[string]string{
		"会议纪要.md":      "# 产品周会纪要\n\n时间：周四上午十点\n地点：三楼会议室\n\n## 结论\n- 下周发布 0.2 版本\n- 搜索面板改为两行布局\n\n## 待办\n- 整理用户反馈\n",
		"读书笔记.txt":     "《设计心理学》\n好的设计让用户一眼看出能做什么。\n反馈要及时，状态要可见。\n",
		"购物清单.txt":     "牛奶\n鸡蛋\n咖啡豆\n",
		"灵感.txt":       "文件操作要能撤销。\n危险操作先确认。\n",
		"报价单.xlsx":     "PK\x03\x04 demo",
		"IMG_0001.jpg": "\xff\xd8\xff demo",
		"~临时1.tmp":     "tmp",
		"cache.tmp":    "tmp",
		"下载片段.tmp":     "tmp",
	}
	if err := os.MkdirAll(filepath.Join(dir, "图片"), 0o755); err != nil {
		return err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// assistDemoSubmit answers the draft with a script.
func (a *App) assistDemoSubmit() {
	text := strings.TrimSpace(a.assist.draft)
	if text == "" || a.assist.running {
		return
	}
	root := a.cur().path
	if !strings.HasPrefix(filepath.Base(root), "sunvale-demo-") {
		dir, err := os.MkdirTemp("", "sunvale-demo-")
		if err == nil {
			err = assistDemoSeed(dir)
		}
		if err != nil {
			a.statusMsg = "无法创建演示文件夹"
			return
		}
		a.navigate(dir)
		root = dir
	}
	a.assist.draft = ""
	a.assist.files = nil
	a.assist.folder = root
	a.assist.turns = append(a.assist.turns, aiTurn{Role: "user", Text: text, At: time.Now()})
	a.assist.running = true
	a.assist.status = "正在思考…"
	a.assistDemoPlay(a.assistDemoScript(text, root))
}

// assistDemoPlay runs beats up to the first gate, on a timer in the app
// and inline under assistDemoSync.
func (a *App) assistDemoPlay(beats []demoBeat) {
	if assistDemoSync {
		for n, b := range beats {
			b.do(a)
			if b.gate != "" {
				a.assist.gateDemo(b.gate, beats[n+1:])
				return
			}
		}
		return
	}
	go func() {
		for n, b := range beats {
			time.Sleep(b.wait)
			done := make(chan struct{})
			a.postAssist(func() { b.do(a); close(done) })
			<-done
			if b.gate != "" {
				a.postAssist(func() { a.assist.gateDemo(b.gate, beats[n+1:]) })
				return
			}
		}
	}()
}

// demoTurn appends a line and returns its index.
func (a *App) demoTurn(t aiTurn) int {
	t.At = time.Now()
	a.assist.turns = append(a.assist.turns, t)
	return len(a.assist.turns) - 1
}

func (a *App) demoOp(i int) *assistant.FileOp { return a.assist.turns[i].Op }

func (a *App) demoSay(text string) demoBeat {
	return demoBeat{wait: 500 * time.Millisecond, do: func(a *App) { a.demoTurn(aiTurn{Role: "assistant", Text: text}) }}
}

// demoEnd closes the run the way runAssistant does: lights cool, the
// listing reloads once the slides are over.
func demoEnd() demoBeat {
	return demoBeat{wait: 200 * time.Millisecond, do: func(a *App) {
		a.assist.running = false
		a.assist.status = ""
		a.assist.flowCool = time.Now().Add(assistFlowCool)
		aiFx.endRun()
		a.reloadAfterFx()
	}}
}

func (a *App) demoSetStep(plan, n int, st assistant.OpStatus) {
	if plan >= 0 && n < len(a.assist.turns[plan].Steps) {
		a.assist.turns[plan].Steps[n].Status = st
	}
}

func (a *App) assistDemoScript(text, root string) []demoBeat {
	switch {
	case strings.Contains(text, "总结") || strings.Contains(text, "摘要"):
		return a.demoSummary(root)
	case strings.Contains(text, "批量"):
		return a.demoBatch(root)
	case strings.Contains(text, "删除") || strings.Contains(text, "清理"):
		return a.demoDelete(root)
	case strings.Contains(text, "重命名") || strings.Contains(text, "归类") || strings.Contains(text, "移动"):
		return a.demoRenameMove(root)
	case strings.Contains(text, "修改") || strings.Contains(text, "改成") || strings.Contains(text, "编辑"):
		return a.demoEdit(root)
	case strings.Contains(text, "新建") || strings.Contains(text, "创建"):
		return a.demoNew(root)
	}
	return []demoBeat{
		a.demoSay("演示模式只认识几类请求：新建文件、修改内容、重命名并归类、删除临时文件、批量加前缀、总结文档。换一种说法试试。"),
		demoEnd(),
	}
}

func (a *App) demoNew(root string) []demoBeat {
	idNew := "new-" + itoa(len(a.assist.turns))
	path := filepath.Join(root, "周报草稿.md")
	body := "# 本周周报\n\n## 完成\n- 助手聊天窗口改版\n- 文件操作卡片与撤销\n\n## 下周\n- 补充批量操作的进度提示\n"
	var at int
	return []demoBeat{
		{wait: 600 * time.Millisecond, do: func(a *App) {
			at = a.demoTurn(aiTurn{Role: "tool", Tool: "write_file", Op: &assistant.FileOp{
				ID: idNew, Kind: assistant.OpCreate, Path: path, After: body, Status: assistant.OpRunning}})
			a.assist.opStart(idNew)
			a.assist.status = "正在写入文件…"
		}},
		{wait: 700 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				op.Status, op.Err = assistant.OpFailed, "无法写入文件，请检查文件夹权限。"
				return
			}
			aiFx.markBusy(path)
			a.reload()
			op.Status = assistant.OpDone
			a.assist.setUndo(idNew, func() error { return os.Remove(path) })
		}},
		a.demoSay("已新建 **周报草稿.md**，包含“完成”和“下周”两节。需要的话可以继续让我补充内容。"),
		demoEnd(),
	}
}

func (a *App) demoEdit(root string) []demoBeat {
	idEdit := "edit-" + itoa(len(a.assist.turns))
	path := filepath.Join(root, "会议纪要.md")
	var at int
	var before string
	return []demoBeat{
		{wait: 500 * time.Millisecond, do: func(a *App) {
			data, _ := os.ReadFile(path)
			before = string(data)
			after := strings.Replace(before, "时间：周四上午十点", "时间：周五下午三点", 1)
			after = strings.Replace(after, "- 整理用户反馈\n", "- 整理用户反馈\n- 周五前确认发布清单\n", 1)
			at = a.demoTurn(aiTurn{Role: "tool", Tool: "edit_file", Op: &assistant.FileOp{
				ID: idEdit, Kind: assistant.OpEdit, Path: path, Before: before, After: after, Status: assistant.OpRunning}})
			a.assist.opStart(idEdit)
			aiFx.markBusy(path)
			a.assist.status = "正在修改内容…"
		}},
		{wait: 800 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			if err := os.WriteFile(path, []byte(op.After), 0o644); err != nil {
				op.Status, op.Err = assistant.OpFailed, "无法写入文件，请检查文件是否被占用。"
				return
			}
			op.Status = assistant.OpDone
			a.assist.setUndo(idEdit, func() error { return os.WriteFile(path, []byte(before), 0o644) })
		}},
		a.demoSay("已把会议时间改为**周五下午三点**，并在待办里补了一条“周五前确认发布清单”。"),
		demoEnd(),
	}
}

func (a *App) demoRenameMove(root string) []demoBeat {
	idRen := "ren-" + itoa(len(a.assist.turns))
	idMov := "mov-" + itoa(len(a.assist.turns))
	src := filepath.Join(root, "IMG_0001.jpg")
	named := filepath.Join(root, "2026-10-18 海边.jpg")
	dst := filepath.Join(root, "图片", "2026-10-18 海边.jpg")
	plan, ren, mov := -1, -1, -1
	return []demoBeat{
		{wait: 400 * time.Millisecond, do: func(a *App) {
			plan = a.demoTurn(aiTurn{Role: "tool", Tool: "plan", Steps: []assistant.PlanStep{
				{Text: "读取 IMG_0001.jpg 的拍摄日期", Status: assistant.OpRunning},
				{Text: "按“日期 地点”重命名"},
				{Text: "移动到“图片”文件夹"},
			}})
			aiFx.markBusy(src)
			a.assist.status = "正在读取拍摄信息…"
		}},
		{wait: 700 * time.Millisecond, do: func(a *App) {
			a.demoSetStep(plan, 0, assistant.OpDone)
			a.demoSetStep(plan, 1, assistant.OpRunning)
			ren = a.demoTurn(aiTurn{Role: "tool", Tool: "rename", Op: &assistant.FileOp{
				ID: idRen, Kind: assistant.OpRename, Path: src, To: named, Status: assistant.OpRunning}})
			a.assist.opStart(idRen)
		}},
		{wait: 600 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(ren)
			if err := os.Rename(src, named); err != nil {
				op.Status, op.Err = assistant.OpFailed, "无法重命名：目标名称已存在或文件被占用。"
				a.demoSetStep(plan, 1, assistant.OpFailed)
				return
			}
			aiFx.markRenamed(src, named)
			a.reload()
			op.Status = assistant.OpDone
			a.assist.setUndo(idRen, func() error { return os.Rename(named, src) })
			a.demoSetStep(plan, 1, assistant.OpDone)
			a.demoSetStep(plan, 2, assistant.OpRunning)
			mov = a.demoTurn(aiTurn{Role: "tool", Tool: "move", Op: &assistant.FileOp{
				ID: idMov, Kind: assistant.OpMove, Path: named, To: dst, Status: assistant.OpRunning}})
			a.assist.opStart(idMov)
			aiFx.markBusy(named)
		}},
		{wait: 700 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(mov)
			if err := fsops.PlaceItem(named, dst, true); err != nil {
				op.Status, op.Err = assistant.OpFailed, "无法移动到“图片”文件夹。"
				a.demoSetStep(plan, 2, assistant.OpFailed)
				return
			}
			aiFx.markLeaving(named)
			op.Status = assistant.OpDone
			a.assist.setUndo(idMov, func() error { return os.Rename(dst, named) })
			a.demoSetStep(plan, 2, assistant.OpDone)
		}},
		a.demoSay("照片拍摄于 2026/10/18，已改名为 **2026-10-18 海边.jpg** 并归类到“图片”文件夹。两步都可以单独撤销。"),
		demoEnd(),
	}
}

// demoGlob lists root's files with ext, sorted by name.
func demoGlob(root, ext string) []string {
	ents, _ := os.ReadDir(root)
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ext) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) demoDelete(root string) []demoBeat {
	idDel := "del-" + itoa(len(a.assist.turns))
	tmps := demoGlob(root, ".tmp")
	items := make([]assistant.OpItem, len(tmps))
	for n, p := range tmps {
		items[n] = assistant.OpItem{From: p}
	}
	var at int
	return []demoBeat{
		{wait: 500 * time.Millisecond, do: func(a *App) {
			at = a.demoTurn(aiTurn{Role: "tool", Tool: "delete", Op: &assistant.FileOp{
				ID: idDel, Kind: assistant.OpDelete, Path: root, Items: items, Danger: true, Status: assistant.OpPending,
				Note: "将把 " + itoa(len(items)) + " 个临时文件移到回收站。确认后执行，之后仍可撤销。"}})
			aiFx.markBusy(tmps...)
			a.assist.running = false
			a.assist.status = ""
		}, gate: idDel},
		{wait: 300 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			op.Status = assistant.OpRunning
			a.assist.opStart(idDel)
			a.assist.running = true
			a.assist.status = "正在移到回收站…"
		}},
		{wait: 500 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			var trashed []fsops.Move
			for n := range op.Items {
				it := &op.Items[n]
				dst, _, err := moveToTrash(it.From)
				if err != nil {
					op.Status, op.Err = assistant.OpFailed, "有文件没能移到回收站，可能正被其他程序占用。"
					break
				}
				it.Done = true
				trashed = append(trashed, fsops.Move{From: it.From, To: dst})
				aiFx.markLeaving(it.From)
			}
			if op.Status != assistant.OpFailed {
				op.Status = assistant.OpDone
				op.Note = ""
			}
			a.assist.setUndo(idDel, func() error {
				for _, m := range trashed {
					if err := sysTrashRestore(m.To, m.From); err != nil {
						return err
					}
				}
				return nil
			})
		}},
		a.demoSay("已把 " + itoa(len(items)) + " 个临时文件移到回收站。点卡片上的“撤销”可以放回原处。"),
		demoEnd(),
	}
}

func (a *App) demoBatch(root string) []demoBeat {
	idBatch := "batch-" + itoa(len(a.assist.turns))
	txts := demoGlob(root, ".txt")
	items := make([]assistant.OpItem, len(txts))
	for n, p := range txts {
		items[n] = assistant.OpItem{From: p, To: filepath.Join(root, "2026-10-23 "+filepath.Base(p))}
	}
	plan, at := -1, -1
	beats := []demoBeat{
		{wait: 400 * time.Millisecond, do: func(a *App) {
			plan = a.demoTurn(aiTurn{Role: "tool", Tool: "plan", Steps: []assistant.PlanStep{
				{Text: "找出当前文件夹里的 .txt 笔记", Status: assistant.OpDone},
				{Text: "逐个加上“2026-10-23 ”前缀", Status: assistant.OpRunning},
				{Text: "检查重名并汇报"},
			}})
			at = a.demoTurn(aiTurn{Role: "tool", Tool: "batch_rename", Op: &assistant.FileOp{
				ID: idBatch, Kind: assistant.OpBatch, Path: root, Items: items, Status: assistant.OpRunning}})
			a.assist.opStart(idBatch)
			aiFx.markBusy(txts...)
			a.assist.status = "正在批量重命名…"
		}},
	}
	var done []fsops.Move
	for n := range items {
		beats = append(beats, demoBeat{wait: 450 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			it := &op.Items[n]
			if err := os.Rename(it.From, it.To); err != nil {
				op.Status, op.Err = assistant.OpFailed, "“"+filepath.Base(it.From)+"”无法重命名。"
				return
			}
			it.Done = true
			done = append(done, fsops.Move{From: it.From, To: it.To})
			aiFx.markRenamed(it.From, it.To)
			a.reload()
		}})
	}
	beats = append(beats,
		demoBeat{wait: 300 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			if op.Status == assistant.OpRunning {
				op.Status = assistant.OpDone
			}
			a.demoSetStep(plan, 1, assistant.OpDone)
			a.demoSetStep(plan, 2, assistant.OpDone)
			a.assist.setUndo(idBatch, func() error {
				for n := len(done) - 1; n >= 0; n-- {
					if err := os.Rename(done[n].To, done[n].From); err != nil {
						return err
					}
				}
				return nil
			})
		}},
		a.demoSay("已给 "+itoa(len(items))+" 个笔记加上日期前缀，没有重名。整批可以一次撤销。"),
		demoEnd(),
	)
	return beats
}

func (a *App) demoSummary(root string) []demoBeat {
	idRead := "read-" + itoa(len(a.assist.turns))
	docs := append(demoGlob(root, ".md"), demoGlob(root, ".txt")...)
	items := make([]assistant.OpItem, len(docs))
	for n, p := range docs {
		items[n] = assistant.OpItem{From: p}
	}
	var at int
	return []demoBeat{
		{wait: 400 * time.Millisecond, do: func(a *App) {
			at = a.demoTurn(aiTurn{Role: "tool", Tool: "read_files", Op: &assistant.FileOp{
				ID: idRead, Kind: assistant.OpRead, Path: root, Items: items, Status: assistant.OpRunning}})
			a.assist.opStart(idRead)
			aiFx.markBusy(docs...)
			a.assist.status = "正在读取文档…"
		}},
		{wait: 900 * time.Millisecond, do: func(a *App) {
			op := a.demoOp(at)
			var b strings.Builder
			b.WriteString("这个文件夹里有 " + itoa(len(docs)) + " 份文本文档：\n\n")
			for n := range op.Items {
				op.Items[n].Done = true
				data, _ := os.ReadFile(op.Items[n].From)
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				head := strings.TrimLeft(lines[0], "# ")
				b.WriteString("- **" + filepath.Base(op.Items[n].From) + "**：" + head + "，共 " + itoa(len(lines)) + " 行\n")
			}
			b.WriteString("\n要点：会议纪要记录了下周发布 0.2 版本；其余是个人笔记与清单，没有需要处理的待办。")
			op.Status = assistant.OpDone
			a.demoTurn(aiTurn{Role: "assistant", Text: b.String()})
		}},
		demoEnd(),
	}
}
