package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/searchquery"
)

// assistant_tools.go is the assistant's wider reach: read the files the user
// named, search and skim text, write, summarize a folder, and ask the user
// to pick. The walk is capped so one question cannot scan a whole disk.

const assistWalkNodes = 3000

// assistSearchHits is how many paths one search_names call returns before
// stopping. The distribution note (see hitDistribution) matters as much as
// the list: without it a deep tree full of matches reads as "the folder has
// N such files", when they may all sit under one subfolder.
const assistSearchHits = 200

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

// readFiles answers with every kind of file, not just text: a picture
// arrives as a picture, a clip as sampled frames. The extraction lives in
// assistant_files.go so the same read backs the composer's attachments.

type pathsArgs struct {
	Paths []string `json:"paths" jsonschema:"要读的路径，相对当前文件夹或绝对路径，最多 12 个"`
}

func (s *sunvaleTools) readFiles(ctx context.Context, _ *mcp.CallToolRequest, args pathsArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Paths) == 0 {
		return mcpErr(fmt.Errorf("paths 不能为空")), nil, nil
	}
	var b strings.Builder
	n := 0
	for _, raw := range args.Paths {
		if ctx.Err() != nil {
			b.WriteString("（已停止）\n")
			break
		}
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
		// Reading a file lights its row's breath until the run ends.
		aiFx.markBusy(path)
		noteAssistRead(path)
		fmt.Fprintf(&b, "%s\n---\n", readAnyFile(path, s.mediaBag()))
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
	// total keeps counting past the path cap: "200 shown" alone would read
	// as the whole answer, and a full count is what keeps the summary honest.
	total := 0
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
		if !query.Matches(d.Name(), rel, searchquery.MatchInfo{IsDir: d.IsDir(), Ext: ext, Size: -1}) {
			return nil
		}
		total++
		if len(hits) < assistSearchHits {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		return mcpErr(err), nil, nil
	}
	text := joinHits("没有匹配的名称", hits, false)
	if dist := hitDistribution(start, hits, total); dist != "" {
		text += dist
	} else if stopped {
		text += "（结果在上限处停下）\n"
	}
	if stopped && total > len(hits) {
		text += "（遍历在节点上限处停下，总数只含已扫描的部分）\n"
	}
	return mcpText(text), nil, nil
}

// hitDistribution summarizes where hits sit: the total found and, per
// top-level child of start, how many hits are under it. That is the
// difference between "the folder has 2825 java files" and "they all live
// under south-plus-app/decompiled". Paths are relative to start.
//
// It speaks up in two situations: the walk stopped with more hits than
// shown, or the hits concentrate — the top level itself holds none and one
// child holds most of them. A flat handful of matches stays bare; a count
// line under every hit list would be noise.
func hitDistribution(start string, hits []string, total int) string {
	if len(hits) == 0 {
		return ""
	}
	counts := map[string]int{}
	// topLevel counts hits sitting directly in start; counts' keys are first
	// components and so can never say whether the top level itself matched.
	topLevel := 0
	for _, h := range hits {
		rel, err := filepath.Rel(start, h)
		if err != nil {
			continue
		}
		if i := strings.Index(rel, string(filepath.Separator)); i >= 0 {
			counts[rel[:i]]++
		} else {
			counts[rel]++
			topLevel++
		}
	}
	concentrated := false
	if topLevel == 0 {
		// Nothing at the top itself: every hit hides under some child, the
		// exact shape that reads as "the folder has N such files".
		largest := 0
		for _, n := range counts {
			if n > largest {
				largest = n
			}
		}
		concentrated = largest*10 >= len(hits)*9
	}
	if total <= len(hits) && !concentrated {
		return ""
	}
	tops := make([]string, 0, len(counts))
	for top := range counts {
		tops = append(tops, top)
	}
	sort.Strings(tops)
	var b strings.Builder
	if total > len(hits) {
		fmt.Fprintf(&b, "共找到 %d 个（显示了前 %d 个）\n", total, len(hits))
	}
	b.WriteString("命中按第一层位置分布：\n")
	for _, top := range tops {
		fmt.Fprintf(&b, "  %s  %d\n", top, counts[top])
	}
	return b.String()
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
		s.noteUndo(undoOp{Desc: "新建文本", Created: []string{path}})
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
	// perTop counts files under each top-level child, so a capped summary
	// shows where the mass sits instead of leaving "files: N" to be read as
	// spread evenly over the tree.
	perTop := map[string]int{}
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
		if rel, err := filepath.Rel(root, path); err == nil {
			top := rel
			if i := strings.Index(rel, string(filepath.Separator)); i >= 0 {
				top = rel[:i]
			}
			perTop[top]++
		}
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
	// The distribution answers "where does the mass sit" whenever one child
	// holds it, capped walk or not — a clean top level over one heavy
	// subfolder is what gets read as "the folder has N files".
	if tops := topCounts(perTop, files); tops != "" {
		b.WriteString(tops)
	}
	return mcpText(b.String()), nil, nil
}

// topCounts renders the per-top-level-child file counts, largest first.
// Only worth showing when the mass concentrates somewhere — over half the
// files under one child — and there is more than one place to compare:
// even splits add noise without changing what the summary says.
func topCounts(perTop map[string]int, files int) string {
	if len(perTop) < 2 || files == 0 {
		return ""
	}
	type pair struct {
		name string
		n    int
	}
	list := make([]pair, 0, len(perTop))
	for name, n := range perTop {
		list = append(list, pair{name, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n == list[j].n {
			return list[i].name < list[j].name
		}
		return list[i].n > list[j].n
	})
	if list[0].n*2 < files {
		return ""
	}
	var b strings.Builder
	b.WriteString("文件按第一层位置分布（大→小）：\n")
	for _, p := range list {
		fmt.Fprintf(&b, "  %s  %d\n", p.name, p.n)
	}
	return b.String()
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
	Prompt  string `json:"prompt" jsonschema:"问用户的问题"`
	Mode    string `json:"mode,omitempty" jsonschema:"multi 可多选，single 只能选一个，默认 multi"`
	Options any    `json:"options" jsonschema:"必须是 JSON 字符串数组，包含 2 到 6 条可执行的做法"`
}

func (s *sunvaleTools) offerChoices(_ context.Context, _ *mcp.CallToolRequest, args choiceArgs) (*mcp.CallToolResult, any, error) {
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return mcpText("offer_choices 需要 prompt：一句问用户的话。请带上 prompt 和 2 到 6 个 options 再调用一次。"), nil, nil
	}
	options := cleanChoiceOptions(choiceOptionValues(args.Options))
	multi := true
	switch strings.ToLower(strings.TrimSpace(args.Mode)) {
	case "single", "one", "单选":
		multi = false
	}
	// Fewer than two options is not a failed tool call. IsError aborts the
	// whole run (NodeRunError) instead of letting the model ask again, so
	// the user sees a stack rather than a choice. Say what to write.
	if len(options) < 2 {
		return mcpText("offer_choices 没有得到 2 个不同的选项。请再调用一次：options 用 JSON 字符串数组，2 到 6 条，每条一句能立刻执行的做法。"), nil, nil
	}
	return mcpText(formatChoices(prompt, multi, options)), nil, nil
}

// choiceOptionValues reads options from whatever shape the model wrote:
// a string array, objects with a label/text/name key, one option per line,
// a JSON array stuffed into a string, or numbers as their text.
func choiceOptionValues(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, optionText(value)...)
		}
		return out
	case string:
		return splitOptionText(values)
	case map[string]any:
		return optionText(values)
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(values, &decoded) != nil {
			return splitOptionText(string(values))
		}
		return choiceOptionValues(decoded)
	default:
		return nil
	}
}

func splitOptionText(values string) []string {
	values = strings.TrimSpace(values)
	if values == "" {
		return nil
	}
	if strings.HasPrefix(values, "[") {
		var decoded any
		if json.Unmarshal([]byte(values), &decoded) == nil {
			if got := choiceOptionValues(decoded); len(got) > 0 {
				return got
			}
		}
	}
	lines := strings.Split(values, "\n")
	if len(lines) == 1 {
		for _, sep := range []string{"、", "；", "|", "，", ","} {
			if strings.Contains(values, sep) {
				lines = strings.Split(values, sep)
				break
			}
		}
	}
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// optionText flattens one option value: a string, an object with a
// label/text/name/value/title key, or a number as its text.
func optionText(value any) []string {
	switch v := value.(type) {
	case string:
		return splitOptionText(v)
	case map[string]any:
		for _, key := range []string{"label", "text", "name", "value", "title"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return []string{strings.TrimSpace(s)}
			}
		}
		return nil
	case float64:
		return []string{strconv.FormatFloat(v, 'f', -1, 64)}
	case json.Number:
		return []string{v.String()}
	case bool:
		return []string{strconv.FormatBool(v)}
	default:
		return nil
	}
}

// cleanChoiceOptions dedupes, trims and caps the choices. Too few options
// is the caller's problem to answer in plain text, not an error.
func cleanChoiceOptions(in []string) []string {
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
	return out
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
