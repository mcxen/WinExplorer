package main

// assistant_tools_more.go is write_text, folder_summary, disk_space,
// current_selection, navigate_to, and offer_choices (plus choice parsing).
// Split from assistant_tools.go for maintainability (LAY2).

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
