package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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

func (a *App) assistDemoScript(text, root string) []demoBeat {
	_, _ = text, root
	return []demoBeat{{do: func(a *App) {
		a.demoTurn(aiTurn{Role: "assistant", Text: "演示场景尚未接入。"})
	}}, demoEnd()}
}
