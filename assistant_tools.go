package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/searchquery"
)

// assistant_tools.go is the assistant's wider reach: read the files the user
// named, search and skim text, write, summarize a folder, and ask the user
// to pick. The walk is capped so one question cannot scan a whole disk.

const (
	assistReadBytes = 32 * 1024
	assistReadFiles = 12
	assistNoteBytes = 48 * 1024
	assistWalkNodes = 3000
)

// errWalkStop ends a capped walk with whatever has been collected.
var errWalkStop = errors.New("stop walk")

func (s *sunvaleTools) planIDs() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.plans))
	for id := range s.plans {
		out[id] = true
	}
	return out
}

func (s *sunvaleTools) plansSince(before map[string]bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id := range s.plans {
		if !before[id] {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *sunvaleTools) drainNav() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.nav
	s.nav = ""
	return path
}

func (s *sunvaleTools) noteNav(path string) {
	s.mu.Lock()
	s.nav = path
	s.mu.Unlock()
}

func (s *sunvaleTools) autopilotOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autopilot
}

func (s *sunvaleTools) selectedPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.selected...)
}

// readTextHead reads the start of a text file. A large file is not an error:
// the user asked to read it, and the rest can be fetched again if needed.
func readTextHead(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("这是文件夹")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, assistReadBytes)
	n, err := f.Read(buf)
	if n == 0 && err != nil && err != io.EOF {
		return "", err
	}
	if bytes.Contains(buf[:n], []byte{0}) {
		return "", fmt.Errorf("不是文本文件")
	}
	text := string(buf[:n])
	if info.Size() > int64(n) {
		text += fmt.Sprintf("\n…已截断，文件共 %d 字节", info.Size())
	}
	return text, nil
}

// specifiedFileNote is the excerpt attached to the user message, so a model
// that forgets to call read_files still sees what was asked for. The budget
// keeps a dozen large files from becoming the whole context.
func specifiedFileNote(paths []string) string {
	var b strings.Builder
	b.WriteString("用户指定的文件，回答前先看这里；不够再调用 read_files：\n")
	used := 0
	for _, raw := range paths {
		piece, err := readTextHead(raw)
		if err != nil {
			piece = err.Error()
		}
		block := raw + "\n" + piece + "\n---\n"
		if used > 0 && used+len(block) > assistNoteBytes {
			b.WriteString(raw)
			b.WriteString("\n（超出本次附带上限，请用 read_files 读取）\n")
			continue
		}
		b.WriteString(block)
		used += len(block)
	}
	return b.String()
}

type pathsArgs struct {
	Paths []string `json:"paths" jsonschema:"要读的路径，相对当前文件夹或绝对路径，最多 12 个"`
}

func (s *sunvaleTools) readFiles(_ context.Context, _ *mcp.CallToolRequest, args pathsArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Paths) == 0 {
		return mcpErr(fmt.Errorf("paths 不能为空")), nil, nil
	}
	var b strings.Builder
	n := 0
	for _, raw := range args.Paths {
		if n >= assistReadFiles {
			b.WriteString("（其余路径未读，一次最多 12 个）\n")
			break
		}
		n++
		path, err := s.resolveEither(raw)
		if err != nil {
			fmt.Fprintf(&b, "%s\n%s\n---\n", raw, err.Error())
			continue
		}
		text, err := readTextHead(path)
		if err != nil {
			fmt.Fprintf(&b, "%s\n%s\n---\n", path, err.Error())
			continue
		}
		fmt.Fprintf(&b, "%s\n%s\n---\n", path, text)
	}
	return mcpText(b.String()), nil, nil
}

type queryArgs struct {
	Query string `json:"query" jsonschema:"文件名查询，语法与搜索框相同"`
	Path  string `json:"path,omitempty" jsonschema:"从哪里找，相对当前文件夹或绝对路径；空表示当前文件夹"`
}

func (s *sunvaleTools) searchNames(ctx context.Context, _ *mcp.CallToolRequest, args queryArgs) (*mcp.CallToolResult, any, error) {
	qtext := strings.TrimSpace(args.Query)
	if qtext == "" {
		return mcpErr(fmt.Errorf("query 不能为空")), nil, nil
	}
	start, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(start)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if !info.IsDir() {
		start = filepath.Dir(start)
	}
	_, hidden := s.bound()
	query := searchquery.Parse(qtext)
	var hits []string
	stopped, err := walkFiles(ctx, start, hidden, assistWalkNodes, func(path string, d fs.DirEntry) error {
		if path == start {
			return nil
		}
		ext := ""
		if !d.IsDir() {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		}
		rel, relErr := filepath.Rel(start, path)
		if relErr != nil {
			rel = path
		}
		if !query.Matches(d.Name(), rel, ext, d.IsDir()) {
			return nil
		}
		hits = append(hits, path)
		if len(hits) >= 50 {
			return errWalkStop
		}
		return nil
	})
	if err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText(joinHits("没有匹配的名称", hits, stopped)), nil, nil
}

type grepArgs struct {
	Query         string `json:"query" jsonschema:"要找的文字"`
	Path          string `json:"path,omitempty" jsonschema:"文件或文件夹，相对当前文件夹或绝对路径"`
	CaseSensitive bool   `json:"case_sensitive,omitempty" jsonschema:"true 时区分大小写"`
}

func (s *sunvaleTools) grepText(ctx context.Context, _ *mcp.CallToolRequest, args grepArgs) (*mcp.CallToolResult, any, error) {
	needle := strings.TrimSpace(args.Query)
	if needle == "" {
		return mcpErr(fmt.Errorf("query 不能为空")), nil, nil
	}
	if len(needle) > 200 {
		return mcpErr(fmt.Errorf("query 太长")), nil, nil
	}
	start, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(start)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	want := needle
	if !args.CaseSensitive {
		want = strings.ToLower(needle)
	}
	var hits []string
	if !info.IsDir() {
		_, _ = grepFile(start, want, args.CaseSensitive, &hits, 40)
		return mcpText(joinHits("没有匹配的文字", hits, false)), nil, nil
	}
	_, hidden := s.bound()
	scanned := 0
	stopped, err := walkFiles(ctx, start, hidden, assistWalkNodes, func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		scanned++
		if scanned > 400 {
			return errWalkStop
		}
		full, gerr := grepFile(path, want, args.CaseSensitive, &hits, 40)
		if gerr != nil {
			return nil
		}
		if full {
			return errWalkStop
		}
		return nil
	})
	if err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText(joinHits("没有匹配的文字", hits, stopped)), nil, nil
}

func grepFile(path, want string, cs bool, hits *[]string, max int) (bool, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 1<<20 || fileLooksBinary(path) {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		hay := sc.Text()
		if !cs {
			hay = strings.ToLower(hay)
		}
		if !strings.Contains(hay, want) {
			continue
		}
		*hits = append(*hits, fmt.Sprintf("%s:%d: %s", path, lineNo, trimRunes(sc.Text(), 180)))
		if len(*hits) >= max {
			return true, nil
		}
	}
	return false, nil
}

func fileLooksBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	return bytes.Contains(buf[:n], []byte{0})
}

type writeArgs struct {
	Path    string `json:"path" jsonschema:"相对当前文件夹或绝对路径"`
	Content string `json:"content" jsonschema:"要写入的文本"`
	Append  bool   `json:"append,omitempty" jsonschema:"true 时追加到已有文件"`
}

func (s *sunvaleTools) writeText(_ context.Context, _ *mcp.CallToolRequest, args writeArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Content) > 256*1024 {
		return mcpErr(fmt.Errorf("一次最多写 256KB")), nil, nil
	}
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if err := refuseRootish(path); err != nil {
		return mcpErr(err), nil, nil
	}
	if secretish(path) {
		return mcpErr(fmt.Errorf("不会改写密钥或凭据文件")), nil, nil
	}
	existed := false
	if info, err := os.Lstat(path); err == nil {
		if info.IsDir() {
			return mcpErr(fmt.Errorf("这是文件夹")), nil, nil
		}
		existed = true
	} else if !os.IsNotExist(err) {
		return mcpErr(err), nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return mcpErr(err), nil, nil
	}
	if args.Append && existed {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		_, werr := f.Write([]byte(args.Content))
		cerr := f.Close()
		if werr != nil {
			return mcpErr(werr), nil, nil
		}
		if cerr != nil {
			return mcpErr(cerr), nil, nil
		}
		return mcpText("已追加到 " + path), nil, nil
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return mcpErr(err), nil, nil
	}
	if !existed {
		s.noteUndo(undoOp{desc: "新建文本", created: []string{path}})
		return mcpText("已新建 " + path), nil, nil
	}
	return mcpText("已覆盖 " + path + "。撤销不会恢复原文。"), nil, nil
}

func (s *sunvaleTools) folderSummary(ctx context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	root, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if !info.IsDir() {
		return mcpErr(fmt.Errorf("需要文件夹")), nil, nil
	}
	_, hidden := s.bound()
	files, dirs := 0, 0
	var total int64
	exts := map[string]int{}
	stopped, err := walkFiles(ctx, root, hidden, assistWalkNodes, func(path string, d fs.DirEntry) error {
		if path == root {
			return nil
		}
		if d.IsDir() {
			dirs++
			return nil
		}
		files++
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(d.Name()), "."))
		if ext == "" {
			ext = "无扩展名"
		}
		exts[ext]++
		return nil
	})
	if err != nil {
		return mcpErr(err), nil, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "路径: %s\n文件: %d\n文件夹: %d\n大小: %s\n", root, files, dirs, formatBytes(total))
	if len(exts) > 0 {
		type pair struct {
			name string
			n    int
		}
		list := make([]pair, 0, len(exts))
		for name, n := range exts {
			list = append(list, pair{name, n})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].n == list[j].n {
				return list[i].name < list[j].name
			}
			return list[i].n > list[j].n
		})
		b.WriteString("扩展名: ")
		limit := len(list)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			if i > 0 {
				b.WriteString("，")
			}
			fmt.Fprintf(&b, "%s %d", list[i].name, list[i].n)
		}
		b.WriteByte('\n')
	}
	if stopped {
		b.WriteString("（统计在上限处停下，不是完整结果）\n")
	}
	return mcpText(b.String()), nil, nil
}

func (s *sunvaleTools) diskSpace(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	free, total, ok := volumeCap(path)
	if !ok {
		return mcpText("读不到这个位置的容量"), nil, nil
	}
	used := total - free
	pct := 0
	if total > 0 {
		pct = int(used * 100 / total)
	}
	text := fmt.Sprintf("路径: %s\n可用: %s\n总共: %s\n已用: %s（%d%%）\n", path, formatBytes(free), formatBytes(total), formatBytes(used), pct)
	return mcpText(text), nil, nil
}

func (s *sunvaleTools) currentSelection(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	paths := s.selectedPaths()
	if len(paths) == 0 {
		return mcpText("没有选中项目"), nil, nil
	}
	extra := 0
	if len(paths) > 100 {
		extra = len(paths) - 100
		paths = paths[:100]
	}
	text := strings.Join(paths, "\n")
	if extra > 0 {
		text += fmt.Sprintf("\n还有 %d 个", extra)
	}
	return mcpText(text), nil, nil
}

func (s *sunvaleTools) navigateTo(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if !info.IsDir() {
		return mcpErr(fmt.Errorf("只能打开文件夹")), nil, nil
	}
	if specialPath(path) {
		return mcpErr(fmt.Errorf("不能打开这个位置")), nil, nil
	}
	s.noteNav(path)
	return mcpText("本轮结束后打开 " + path), nil, nil
}

type choiceArgs struct {
	Prompt  string   `json:"prompt" jsonschema:"问用户的问题"`
	Mode    string   `json:"mode,omitempty" jsonschema:"multi 可多选，single 只能选一个，默认 multi"`
	Options []string `json:"options" jsonschema:"2 到 8 条可执行的做法"`
}

func (s *sunvaleTools) offerChoices(_ context.Context, _ *mcp.CallToolRequest, args choiceArgs) (*mcp.CallToolResult, any, error) {
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return mcpErr(fmt.Errorf("prompt 不能为空")), nil, nil
	}
	options, err := cleanChoiceOptions(args.Options)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	multi := true
	switch strings.ToLower(strings.TrimSpace(args.Mode)) {
	case "single", "one", "单选":
		multi = false
	}
	return mcpText(formatChoices(prompt, multi, options)), nil, nil
}

func cleanChoiceOptions(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, opt := range in {
		opt = strings.TrimSpace(opt)
		if opt == "" || seen[opt] {
			continue
		}
		opt = trimRunes(opt, 120)
		seen[opt] = true
		out = append(out, opt)
		if len(out) == 8 {
			break
		}
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("需要至少 2 个选项")
	}
	return out, nil
}

func joinHits(empty string, hits []string, stopped bool) string {
	var b strings.Builder
	if len(hits) == 0 {
		b.WriteString(empty)
	} else {
		b.WriteString(strings.Join(hits, "\n"))
	}
	b.WriteByte('\n')
	if stopped {
		b.WriteString("（结果在上限处停下）\n")
	}
	return b.String()
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// secretish is the write guard. Reading a file the user named is allowed;
// letting the model rewrite a key or a cookie jar is not.
func secretish(path string) bool {
	s := strings.ToLower(filepath.ToSlash(path))
	for _, part := range []string{"/.ssh/", "/.aws/", "/.gnupg/", "/cookies", ".netrc", "id_rsa", "id_ed25519", ".pem"} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func walkFiles(ctx context.Context, root string, hidden bool, maxNodes int, fn func(path string, d fs.DirEntry) error) (stopped bool, err error) {
	visited := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			stopped = true
			return errWalkStop
		}
		visited++
		if visited > maxNodes {
			stopped = true
			return errWalkStop
		}
		name := d.Name()
		if path != root && !hidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if callErr := fn(path, d); callErr != nil {
			if errors.Is(callErr, errWalkStop) {
				stopped = true
			}
			return callErr
		}
		return nil
	})
	if errors.Is(err, errWalkStop) {
		return stopped, nil
	}
	return stopped, err
}
