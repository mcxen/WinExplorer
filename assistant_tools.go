package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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
