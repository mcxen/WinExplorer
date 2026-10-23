package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// assistant_shell.go is the assistant's bash tool: the app's own shell
// (shellcore.go) running the script the model wrote, inside the folder of
// the tab that asked. The interpreter brings the language — pipes,
// redirects, globs, &&/||, for and if — and the Go coreutils stand in for
// the commands Windows does not ship, so 「按扩展名归档」「数一数有多少
// 张照片」 is one script on every platform instead of a shell-out that
// behaves two ways.
//
// The guardrails: the run's root bounds the commands' own writes (cp, mv,
// rm) and the interpreter's redirections; a timeout and an output cap keep
// a runaway script from taking the conversation down with it. Commands the
// model may still use are the real binaries on PATH — git, ffmpeg, tar —
// and those run wherever the system's shell would run them.

const (
	shellTimeout   = 30 * time.Second
	shellOutputMax = 64 * 1024
	shellPathValue = "/usr/bin:/bin:/usr/local/bin"
)

type bashArgs struct {
	Script string `json:"script" jsonschema:"要执行的 shell 脚本，用 POSIX sh 语法"`
	Why    string `json:"why,omitempty" jsonschema:"一句话说明这个脚本做什么，会显示给用户"`
}

// runBash executes script in root with the app's shell.
func runBash(ctx context.Context, root, script string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("工作目录不存在：%s", root)
		}
		return "", fmt.Errorf("无法打开工作目录：%v", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("工作目录不是文件夹：%s", root)
	}
	out := &cappedWriter{cap: shellOutputMax}
	var stderr strings.Builder
	runner, err := interpRunner(root, out, &stderr)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	prog, perr := syntax.NewParser().Parse(strings.NewReader(script), "bash")
	if perr != nil {
		return "", fmt.Errorf("脚本语法错误：%v", perr)
	}
	err = runner.Run(ctx, prog)
	text := strings.TrimSpace(out.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("脚本超过 %s 秒被停止", shellTimeout.String())
		}
		if errText != "" {
			return text, fmt.Errorf("%s：%s", err.Error(), errText)
		}
		return text, err
	}
	return text, nil
}

// cappedWriter is the run's output ceiling. A full one stops growing rather
// than failing: a model reading the head of an accidental `yes` is better
// than one reading nothing.
type cappedWriter struct {
	cap int
	b   strings.Builder
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.b.Len() >= w.cap {
		return len(p), nil
	}
	left := w.cap - w.b.Len()
	if left > len(p) {
		left = len(p)
	}
	w.b.Write(p[:left])
	return len(p), nil
}

func (w *cappedWriter) String() string {
	s := w.b.String()
	if w.b.Len() >= w.cap {
		s += "\n（输出到达上限已截断）"
	}
	return s
}

// shellRootGuard is the sandbox every path in the run resolves through:
// the command, its redirections and the open handler all ask it first.
type shellRootGuard struct{ root string }

func shellGuardFor(root string) *shellRootGuard {
	abs, err := filepath.Abs(root)
	if err != nil {
		return &shellRootGuard{root: root}
	}
	return &shellRootGuard{root: abs}
}

func (g *shellRootGuard) within(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if p != g.root && !strings.HasPrefix(p, g.root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径 %s 在工作目录之外，已拒绝", path)
	}
	return p, nil
}

// interpRunner builds the interpreter over root: the sandbox for opens and
// the Go coreutils for what Windows lacks. out and errOut take the run's
// output, so the caller owns the ceiling.
func interpRunner(root string, out, errOut io.Writer) (*interp.Runner, error) {
	guard := shellGuardFor(root)
	return interp.New(
		interp.Dir(guard.root),
		interp.Env(expand.ListEnviron(
			"PWD="+guard.root,
			"HOME="+guard.root,
			"PATH="+shellPathValue,
			"SUNVALE=1",
		)),
		interp.ExecHandlers(func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
			return shellExec(next)
		}),
		interp.OpenHandler(guard.open),
		interp.StdIO(nil, out, errOut),
	)
}

// open is the interpreter's every-file gate: redirections and `>` writes
// land here, so the sandbox cannot be stepped out of with `../../etc/passwd`.
func (g *shellRootGuard) open(ctx context.Context, path string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
	if path == "" {
		return nil, fmt.Errorf("空路径")
	}
	p := path
	if !filepath.IsAbs(p) {
		if hctx := interp.HandlerCtx(ctx); hctx.Dir != "" {
			p = filepath.Join(hctx.Dir, p)
		}
	}
	safe, err := g.within(p)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(safe, flag, perm)
}

// shellBashTool is the MCP registration shape.
func registerShellTool(s *sunvaleTools, server *mcp.Server) {
	addSunvaleTool(s, server, &mcp.Tool{
		Name:        "bash",
		Description: "在工作目录里执行一段 POSIX shell 脚本（sh 语法：管道、重定向、通配符、for/if、&&/||）。内置 ls、cat、grep、find、sed、cp、mv、mkdir、rm、sort、wc、head、tail、du、stat、cut、uniq、tr、basename、dirname、date、seq 等命令，Windows 上同样可用；git、ffmpeg 等系统命令也能调用。只能读写工作目录内的路径，有超时。批量整理、统计、重命名前的核对用本工具，比逐个调文件工具快。",
	}, s.bash)
}

func (s *sunvaleTools) bash(_ context.Context, _ *mcp.CallToolRequest, args bashArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	if strings.TrimSpace(args.Script) == "" {
		return mcpErr(fmt.Errorf("script 不能为空")), nil, nil
	}
	out, err := runBash(shellCtx(), root, args.Script)
	if err != nil {
		if out != "" {
			return mcpText(out + "\n" + err.Error()), nil, nil
		}
		return mcpErr(err), nil, nil
	}
	if out == "" {
		return mcpText("执行完了，没有输出。"), nil, nil
	}
	return mcpText(out), nil, nil
}

// shellCtx is a run's background context; the tools carry no cancellation
// of their own beyond the interpreter's timeout.
func shellCtx() context.Context { return context.Background() }
