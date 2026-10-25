package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/egoist/mygo"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/applog"
	assistant "sunvale/internal/assistant"
	"sunvale/internal/rename"
)

// assistant_mcp.go is the in-process MCP server Eino calls. The tools are
// sunvale's own file actions, bound to the folder of the tab that asked.
// Nothing here starts a subprocess.

type sunvaleTools struct {
	mu         sync.Mutex
	root       string
	showHidden bool
	book       []archivePassword
	pending    []archivePassword
	undos      []undoOp
	plans      map[string]aiRenamePlan
	// selected is the tab's selection at the moment the user sent, not a
	// live view of later clicks. autopilot lets this run apply rename plans
	// itself. nav is a folder to open once the turn ends.
	// memory is the facts bound for this run. memAdd and memDrop are what
	// the window should merge when the turn ends; the tool does not write
	// the memory file itself.
	selected  []string
	autopilot bool
	nav       string
	// web is the run's web_search/web_read configuration, bound from
	// prefs.webSearch; webOff is the skill toggle turning them down.
	web    assistant.WebSearch
	webOff bool
	// frequent is the run's snapshot of the usage ledger, backing the
	// frequent_files tool.
	frequent []string
	// media holds the pictures one run extracted. Tool text only carries
	// their ids; the model-side rewrite turns those into real image parts.
	media   *assistMediaBag
	memory  []aiMemory
	memAdd  []aiMemory
	memDrop []string
	// onToolCall is an optional test hook. Production leaves it nil; the
	// real-model regression uses it to record the exact tool sequence.
	onToolCall func(string)
}

var sunvaleToolState = &sunvaleTools{plans: map[string]aiRenamePlan{}}

var (
	mcpOnce       sync.Once
	mcpClient     *mcp.ClientSession
	mcpServerSide *mcp.ServerSession
	mcpConnectErr error
)

// copyText copies ordinary text. Archive passwords are never routed here.
var copyText = func(text string) error {
	if !mygo.App.IsReady() {
		return fmt.Errorf("剪贴板不可用")
	}
	mygo.Clipboard.WriteText(text)
	return nil
}

func sunvaleSession(ctx context.Context) (*mcp.ClientSession, error) {
	mcpOnce.Do(func() {
		server := newSunvaleServer()
		client := mcp.NewClient(&mcp.Implementation{Name: "sunvale-eino", Version: "0.1.0"}, nil)
		ct, st := mcp.NewInMemoryTransports()
		sess, err := server.Connect(ctx, st, nil)
		if err != nil {
			mcpConnectErr = err
			return
		}
		mcpServerSide = sess
		mcpClient, mcpConnectErr = client.Connect(ctx, ct, nil)
	})
	return mcpClient, mcpConnectErr
}

// sunvaleRunTools is the one way runs turn the MCP session into Eino tools.
// The result policy matters as much as the fetch: a tool's IsError result
// (bad script, path refused) is information for the model to correct itself
// on — that is why MCP has the flag — while officialmcp's default turns it
// into a Go error that kills the whole run on the first failed call.
func sunvaleRunTools(ctx context.Context, sess *mcp.ClientSession) ([]tool.BaseTool, error) {
	asError := false
	return officialmcp.GetTools(ctx, &officialmcp.Config{
		Cli:          sess,
		ServerName:   "sunvale",
		ResultPolicy: &officialmcp.ResultPolicy{ErrorAsError: &asError},
	})
}

func closeAssistantMCP() {
	if mcpClient != nil {
		_ = mcpClient.Close()
	}
	if mcpServerSide != nil {
		_ = mcpServerSide.Close()
	}
}

func (s *sunvaleTools) bind(root string, showHidden bool, book []archivePassword, selected []string, autopilot bool) {
	s.mu.Lock()
	s.root = root
	s.showHidden = showHidden
	s.book = append([]archivePassword(nil), book...)
	s.selected = append([]string(nil), selected...)
	s.autopilot = autopilot
	s.pending = nil
	s.undos = nil
	s.nav = ""
	s.mu.Unlock()
}

// bindMedia points the run's read tools at a fresh picture bag.
func (s *sunvaleTools) bindMedia(bag *assistMediaBag) {
	s.mu.Lock()
	s.media = bag
	s.mu.Unlock()
}

func (s *sunvaleTools) mediaBag() *assistMediaBag {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.media
}

// setToolCallHook installs the test-only observer without exposing the
// callback to the model or persisted state.
func (s *sunvaleTools) setToolCallHook(fn func(string)) {
	s.mu.Lock()
	s.onToolCall = fn
	s.mu.Unlock()
}

func (s *sunvaleTools) noteToolCall(name string) {
	s.mu.Lock()
	fn := s.onToolCall
	s.mu.Unlock()
	if fn != nil {
		fn(name)
	}
}

func (s *sunvaleTools) drainPending() []archivePassword {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.pending
	s.pending = nil
	return out
}

func (s *sunvaleTools) noteUndo(op undoOp) {
	s.mu.Lock()
	s.undos = append(s.undos, op)
	s.mu.Unlock()
}

func (s *sunvaleTools) drainUndos() []undoOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.undos
	s.undos = nil
	return out
}

func (s *sunvaleTools) bound() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root, s.showHidden
}

func (s *sunvaleTools) putPlan(p aiRenamePlan) string {
	id := newID()
	s.mu.Lock()
	if s.plans == nil {
		s.plans = map[string]aiRenamePlan{}
	}
	s.plans[id] = p
	s.mu.Unlock()
	return id
}

func (s *sunvaleTools) takePlan(id string) (aiRenamePlan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	return p, ok
}

// resolveInRoot resolves rel against root. Absolute paths are used as given
// and relative paths (including "..") are joined onto root; the assistant may
// reach any path the OS user can.
func resolveInRoot(root, rel string) (string, error) {
	root = filepath.Clean(root)
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return root, nil
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel), nil
	}
	return filepath.Join(root, rel), nil
}

func mcpText(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func mcpErr(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

type pathArgs struct {
	Path string `json:"path,omitempty" jsonschema:"相对当前文件夹的路径，空表示当前文件夹"`
}

func (s *sunvaleTools) listDir(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	root, hidden := s.bound()
	dir, err := resolveInRoot(root, args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	return listDirText(dir, hidden)
}

func (s *sunvaleTools) readFile(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	path, err := resolveInRoot(root, args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	// The row of the file being read breathes until the run ends, and the
	// read scores in the window's usage ledger: what the assistant keeps
	// looking at is part of what the user works with.
	aiFx.markBusy(path)
	noteAssistRead(path)
	return readTextFile(path, s.mediaBag())
}

// readTextFile is named for the common case but reads any file the
// extractor understands: text, documents, pictures, media and archives.
func readTextFile(path string, bag *assistMediaBag) (*mcp.CallToolResult, any, error) {
	return mcpText(readAnyFile(path, bag)), nil, nil
}

type renameArgs struct {
	Mode    string `json:"mode,omitempty" jsonschema:"template、regex 或 find，默认 template"`
	Pattern string `json:"pattern" jsonschema:"模板、正则或要查找的文字。template 可用 {modified_year}-{modified_month}-{modified_date}-{n:3}.{ext} 等日期、序号、名称令牌"`
	Replace string `json:"replace,omitempty" jsonschema:"regex 和 find 的替换文字"`
	Start   int    `json:"start,omitempty" jsonschema:"模板序号起点，默认 1"`
}

func (s *sunvaleTools) renamePlan(_ context.Context, _ *mcp.CallToolRequest, args renameArgs) (*mcp.CallToolResult, any, error) {
	root, hidden := s.bound()
	if strings.TrimSpace(args.Pattern) == "" {
		return mcpErr(fmt.Errorf("pattern 不能为空")), nil, nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	var sources []rename.Source
	for _, e := range ents {
		if !hidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		ext := ""
		if !e.IsDir() {
			ext = strings.TrimPrefix(strings.ToLower(filepath.Ext(e.Name())), ".")
		}
		sources = append(sources, rename.Source{
			Name: e.Name(), Path: filepath.Join(root, e.Name()), IsDir: e.IsDir(), Ext: ext, ModTime: info.ModTime(),
		})
		if len(sources) >= 200 {
			break
		}
	}
	mode := rename.ModeTemplate
	switch strings.ToLower(args.Mode) {
	case "regex":
		mode = rename.ModeRegex
	case "find", "replace":
		mode = rename.ModeFindReplace
	}
	start := args.Start
	if start == 0 {
		start = 1
	}
	rows := rename.RowsFrom(sources)
	opt := rename.Options{Mode: mode, Pattern: args.Pattern, Replace: args.Replace, Start: start}
	rename.Replan(rows, opt, root)
	id := s.putPlan(aiRenamePlan{Dir: root, Rows: rows})
	var b strings.Builder
	fmt.Fprintf(&b, "plan_id: %s\n", id)
	fmt.Fprintf(&b, "文件夹: %s\n", root)
	n := 0
	for _, r := range rows {
		if r.To == r.Name && r.Err == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%s → %s", r.Name, r.To)
		if r.Err != "" {
			fmt.Fprintf(&b, "（%s）", r.Err)
		}
		b.WriteByte('\n')
	}
	if n == 0 {
		b.WriteString("没有需要改名的项目\n")
	}
	if s.autopilotOn() {
		b.WriteString("全托管会在本轮结束时自动应用这个方案。")
	} else {
		b.WriteString("这只是方案。用户点「应用方案」后才会改名。")
	}
	return mcpText(b.String()), nil, nil
}

type zipArgs struct {
	Archive string   `json:"archive" jsonschema:"要创建的 zip 文件名，相对当前文件夹"`
	Names   []string `json:"names" jsonschema:"要打包的名称，相对当前文件夹"`
}

func (s *sunvaleTools) zipFiles(ctx context.Context, _ *mcp.CallToolRequest, args zipArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	dst, err := resolveInRoot(root, args.Archive)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if !strings.EqualFold(filepath.Ext(dst), ".zip") {
		return mcpErr(fmt.Errorf("只创建 .zip")), nil, nil
	}
	var entries []Entry
	for _, name := range args.Names {
		path, err := resolveInRoot(root, name)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		entries = append(entries, Entry{Name: filepath.Base(path), Path: path, IsDir: info.IsDir()})
	}
	if len(entries) == 0 {
		return mcpErr(fmt.Errorf("没有要打包的项目")), nil, nil
	}
	// Queued like the user's own: it waits for a disk slot, then runs.
	_, err = runTaskWait(ctx, archiveSpec("compress", dst, "ai"), archiveRunner("compress", dst, func(t *archiveTask) error {
		t.reset(entriesSize(entries))
		return makeZip(dst, entries)
	}))
	if err != nil {
		_ = os.Remove(dst)
		return mcpErr(err), nil, nil
	}
	return mcpText("已压缩到 " + dst), nil, nil
}

type unzipArgs struct {
	Archive string `json:"archive" jsonschema:"要解压的归档，相对当前文件夹"`
	Dest    string `json:"dest,omitempty" jsonschema:"解压到的新文件夹，相对当前文件夹；空则用归档名"`
}

func (s *sunvaleTools) unzip(ctx context.Context, _ *mcp.CallToolRequest, args unzipArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	src, err := resolveInRoot(root, args.Archive)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	destRel := args.Dest
	if strings.TrimSpace(destRel) == "" {
		base := filepath.Base(src)
		destRel = strings.TrimSuffix(base, filepath.Ext(base))
		if strings.HasSuffix(strings.ToLower(base), ".tar.gz") {
			destRel = strings.TrimSuffix(base, ".tar.gz")
		}
	}
	dst, err := resolveInRoot(root, destRel)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	passwords := archivePasswordCandidates(s.archivePasswordBook())
	aiFx.markBusy(src)
	var nested nestedReport
	_, err = runTaskWait(ctx, archiveSpec("extract", src, "ai"), archiveRunner("extract", src, func(*archiveTask) error {
		var err error
		_, nested, err = extractArchiveNested(src, dst, passwords)
		return err
	}))
	if err != nil {
		_ = os.RemoveAll(dst)
		return mcpErr(err), nil, nil
	}
	noteAssistRead(src)
	msg := "已解压到 " + dst
	if s := nested.summary(); s != "" {
		msg += "\n" + s
		if len(nested.Locked) > 0 {
			msg += "\n用户补充密码（password_save 或 设置 → 助手）后可对未解开的内层归档重试。"
		}
	}
	// The natural next step: keep only what the archive became. In the
	// pane's confirm mode the model asks before touching the original.
	if !s.autopilotOn() {
		msg += "\n下一步：可用 offer_choices（mode=single）询问是否把原归档移入回收站（trash_items），选项如「删除原归档」「保留」。全托管或用户已明确保留时不必问。"
	}
	return mcpText(msg), nil, nil
}

func (s *sunvaleTools) archivePasswordBook() []archivePassword {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(append([]archivePassword(nil), s.book...), s.pending...)
}

func (s *sunvaleTools) passwordList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	book := append([]archivePassword(nil), s.book...)
	s.mu.Unlock()
	if len(book) == 0 {
		return mcpText("解压密码库是空的"), nil, nil
	}
	var b strings.Builder
	for i, p := range book {
		label := p.Label
		if label == "" {
			label = fmt.Sprintf("解压密码 %d", i+1)
		}
		b.WriteString(label)
		b.WriteByte('\n')
	}
	b.WriteString("这里只有名称，没有密码正文。unzip 会按顺序自动试用。")
	return mcpText(b.String()), nil, nil
}

func (s *sunvaleTools) passwordSave(_ context.Context, _ *mcp.CallToolRequest, args pwSaveArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Label) == "" || args.Secret == "" {
		return mcpErr(fmt.Errorf("名称和解压密码不能为空")), nil, nil
	}
	s.mu.Lock()
	s.pending = append(s.pending, archivePassword{ID: newID(), Label: strings.TrimSpace(args.Label), Secret: args.Secret})
	s.mu.Unlock()
	return mcpText("已保存解压密码「" + strings.TrimSpace(args.Label) + "」，密码没有写进回复。"), nil, nil
}

type pwSaveArgs struct {
	Label  string `json:"label" jsonschema:"解压密码的名称，不是账号"`
	Secret string `json:"secret" jsonschema:"解压密码本身，不要复述"`
}

func newSunvaleServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "sunvale", Version: "0.1.0"}, nil)
	s := sunvaleToolState
	addSunvaleTool(s, server, &mcp.Tool{Name: "list_dir", Description: "列出当前文件夹里的项目。path 相对当前文件夹。"}, s.listDir)
	addSunvaleTool(s, server, &mcp.Tool{Name: "read_file", Description: "读取当前文件夹里的一个文件：文本、代码、Word/Excel/PPT/PDF/epub 正文、图片、视频帧、音频、压缩包清单都可以。路径相对当前文件夹，大文件只返回开头。"}, s.readFile)
	addSunvaleTool(s, server, &mcp.Tool{Name: "read_files", Description: "读取用户指定的多个文件，最多 12 个。图片和视频会直接附上画面（模型能看图时），文档返回正文，音视频返回时长与画面帧。路径相对当前文件夹，或绝对路径。"}, s.readFiles)
	addSunvaleTool(s, server, &mcp.Tool{Name: "list_outside", Description: "列出任意绝对路径上的文件夹。"}, s.listOutside)
	addSunvaleTool(s, server, &mcp.Tool{Name: "read_outside", Description: "读取任意绝对路径上的文件，支持的类型与 read_file 相同。"}, s.readOutside)
	addSunvaleTool(s, server, &mcp.Tool{Name: "properties", Description: "查看文件或文件夹的大小、时间和权限。路径相对当前文件夹，或绝对路径。"}, s.properties)
	addSunvaleTool(s, server, &mcp.Tool{Name: "mkdir", Description: "在当前文件夹里新建文件夹。"}, s.mkdir)
	addSunvaleTool(s, server, &mcp.Tool{Name: "new_text_file", Description: "在当前文件夹里新建文本文件。"}, s.newTextFile)
	addSunvaleTool(s, server, &mcp.Tool{Name: "copy_items", Description: "把当前文件夹里的项目复制到目标文件夹。"}, s.copyItems)
	addSunvaleTool(s, server, &mcp.Tool{Name: "move_items", Description: "把当前文件夹里的项目移动到目标文件夹。"}, s.moveItems)
	addSunvaleTool(s, server, &mcp.Tool{Name: "trash_items", Description: "把当前文件夹里的项目移到系统回收站，不是永久删除。用户要求删除或移除时调用本工具。"}, s.trashItems)
	addSunvaleTool(s, server, &mcp.Tool{Name: "rename_item", Description: "立刻重命名当前文件夹里的一个项目。"}, s.renameItem)
	addSunvaleTool(s, server, &mcp.Tool{Name: "open_item", Description: "用系统默认程序打开一个文件或文件夹。"}, s.openItem)
	addSunvaleTool(s, server, &mcp.Tool{Name: "open_terminal", Description: "在该文件夹打开系统终端。"}, s.openTerminal)
	addSunvaleTool(s, server, &mcp.Tool{Name: "copy_as_path", Description: "把项目的绝对路径复制到剪贴板，并在结果里返回路径。"}, s.copyAsPath)
	addSunvaleTool(s, server, &mcp.Tool{Name: "rename_plan", Description: "为当前文件夹生成批量重命名方案，不会真正改名。所有批量改名都调用本工具，模板令牌见参数说明。"}, s.renamePlan)
	addSunvaleTool(s, server, &mcp.Tool{Name: "zip_files", Description: "把当前文件夹里的若干项目打成一个 zip。"}, s.zipFiles)
	addSunvaleTool(s, server, &mcp.Tool{Name: "unzip", Description: "把归档解压到新文件夹，自动按顺序尝试解压密码库。需要密码却没命中时，请用户到设置 → 助手添加。"}, s.unzip)
	addSunvaleTool(s, server, &mcp.Tool{Name: "password_list", Description: "列出解压密码库的名称，不含密码正文。仅用于排查加密归档。"}, s.passwordList)
	addSunvaleTool(s, server, &mcp.Tool{Name: "password_save", Description: "把用户刚提供的解压密码记入密码库，回复里不要复述密码。"}, s.passwordSave)
	addSunvaleTool(s, server, &mcp.Tool{Name: "search_names", Description: "按文件名在文件夹里搜索。query 用搜索框的语法。"}, s.searchNames)
	addSunvaleTool(s, server, &mcp.Tool{Name: "grep_text", Description: "在文本文件里查找一段文字。path 可以是文件或文件夹。"}, s.grepText)
	addSunvaleTool(s, server, &mcp.Tool{Name: "write_text", Description: "把文本写入文件。append 为 true 时追加，否则覆盖。新建可以撤销，覆盖不会恢复原文。"}, s.writeText)
	addSunvaleTool(s, server, &mcp.Tool{Name: "folder_summary", Description: "统计文件夹里的文件数、文件夹数、总大小和常见扩展名。"}, s.folderSummary)
	addSunvaleTool(s, server, &mcp.Tool{Name: "disk_space", Description: "查看路径所在磁盘还剩多少空间。"}, s.diskSpace)
	addSunvaleTool(s, server, &mcp.Tool{Name: "current_selection", Description: "这次发送时用户选中的路径，不含之后新点的项目。"}, s.currentSelection)
	addSunvaleTool(s, server, &mcp.Tool{Name: "frequent_files", Description: "列出用户常用的文件和文件夹（按使用频率与最近程度排序）。用户说「常看的」「之前用过的」「老用的」时先调用本工具，不要猜。"}, s.frequentFiles)
	addSunvaleTool(s, server, &mcp.Tool{Name: "navigate_to", Description: "本轮结束后让窗口打开一个文件夹，本轮里的工具仍用原来的文件夹。"}, s.navigateTo)
	addSunvaleTool(s, server, &mcp.Tool{Name: "present_form", Description: "向用户出示可填写的规范表单（时间、路径、开关、分段等）。缺路径/时间时先调用；调用后停止本轮等用户提交。preset=schedule 可直接给出定时任务表单。"}, s.presentForm)
	addSunvaleTool(s, server, &mcp.Tool{Name: "offer_choices", Description: "把可多选的做法交给用户。用户要求选择或比较方案时必须调用；调用后停止本轮，等用户勾选。"}, s.offerChoices)
	addSunvaleTool(s, server, &mcp.Tool{Name: "remember", Description: "记住一句用户明确要求留下的事实，供以后的会话使用。不要记密码、密钥或文件内容。"}, s.remember)
	addSunvaleTool(s, server, &mcp.Tool{Name: "forget", Description: "按原文忘掉一条记忆。有多条相似时先让用户说完整的一句。"}, s.forget)
	addSunvaleTool(s, server, &mcp.Tool{Name: "memory_list", Description: "列出已经记住的短事实。"}, s.memoryList)
	registerShellTool(s, server)
	registerWebSearchTools(s, server)
	registerScheduleTools(s, server)
	registerAssistantTaskTools(s, server)
	return server
}

// addSunvaleTool wraps a registered handler with the test-only call
// recorder. Keeping the wrapper here means every current and future tool is
// observed by the regression without changing its implementation.
func addSunvaleTool[In, Out any](s *sunvaleTools, server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	name := tool.Name
	mcp.AddTool(server, tool, func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		s.noteToolCall(name)
		start := time.Now()
		defer applog.Recover("mcp tool "+name, true)
		res, out, err := handler(ctx, req, input)
		logMCPCall(name, input, start, res, err)
		return res, out, err
	})
}
