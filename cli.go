package main

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// cli.go 处理命令行：`sunvale <目录>` 打开该目录，`sunvale <文件>` 打开其所在
// 目录并选中它；相对路径按启动目录解析，支持 ~、环境变量和 Windows 路径
// （与地址栏同一套 resolvePathInput）。已有实例时 mygo 的单实例套接字把参数和
// 工作目录转给它（OnSecondInstance），由它在新标签页打开并提到前台。

// cliFlags 处理不需要界面的参数：--version/-v 打印版本，--help/-h 打印用法。
// handled 为真时 main 直接退出，不启动 GUI。
func cliFlags(args []string, out io.Writer) (handled bool) {
	for _, a := range args {
		switch a {
		case "--version", "-v", "-V", "/version":
			v := mygo.App.Version()
			if v == "" {
				v = "dev"
			}
			fmt.Fprintln(out, "Sunvale (晴谷)", v)
			return true
		case "--help", "-h", "/?":
			fmt.Fprintln(out, "用法: sunvale [目录或文件]\n  目录：在其中打开；文件：打开所在目录并选中。\n  --version  打印版本号\n  --help     显示本帮助")
			return true
		}
	}
	return false
}

// cliTargets 把命令行参数解析成要打开的位置；以 - 开头的参数和找不到的路径
// 被忽略（记日志）。
func cliTargets(args []string, wd string) []pathTarget {
	var out []pathTarget
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") || strings.Contains(a, "://") && !strings.HasPrefix(a, "file://") {
			continue
		}
		t, msg := resolvePathInput(a, wd)
		if msg != "" {
			slog.Warn("cli: path not found", "arg", a)
			continue
		}
		out = append(out, t)
	}
	return out
}

// cliHandled 记下命令行已经打开的文件，mygo 随后对同一个压缩包的
// OnOpenFile 就不再另开窗口。
var cliHandled sync.Map

// openCLITargets 在 app 里打开 targets：first 为真时第一个位置替换当前标签页
// （冷启动），其余每个开一个新标签页。
func (a *App) openCLITargets(targets []pathTarget, first bool) {
	for i, t := range targets {
		if i == 0 && first {
			a.navigate(t.dir)
		} else {
			a.openTab(t.dir)
		}
		if t.file != "" {
			cliHandled.Store(t.file, true)
			a.selectOnly(t.file)
		}
	}
}
