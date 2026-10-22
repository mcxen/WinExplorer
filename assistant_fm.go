package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/rename"
)

// assistant_fm.go adds the file-manager actions Explorer already has:
// look outside the open folder, inspect, create, copy, move, trash,
// rename one item, open, and copy a path. Batch rename stays a proposal.

type absArgs struct {
	Path string `json:"path" jsonschema:"绝对路径"`
}

type nameArgs struct {
	Name string `json:"name,omitempty" jsonschema:"名称；新建文件可空"`
}

type renameOneArgs struct {
	Name string `json:"name" jsonschema:"当前文件夹里的现有名称"`
	To   string `json:"to" jsonschema:"新名称，不含路径"`
}

type transferArgs struct {
	Names []string `json:"names" jsonschema:"当前文件夹里的名称"`
	Dest  string   `json:"dest" jsonschema:"目标文件夹，相对当前文件夹或绝对路径"`
}

type namesArgs struct {
	Names []string `json:"names" jsonschema:"当前文件夹里的名称"`
}

func (s *sunvaleTools) listOutside(_ context.Context, _ *mcp.CallToolRequest, args absArgs) (*mcp.CallToolResult, any, error) {
	dir, err := absPath(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	_, hidden := s.bound()
	return listDirText(dir, hidden)
}

func (s *sunvaleTools) readOutside(_ context.Context, _ *mcp.CallToolRequest, args absArgs) (*mcp.CallToolResult, any, error) {
	path, err := absPath(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	return readTextFile(path, s.mediaBag())
}

func (s *sunvaleTools) properties(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	kind := "文件"
	if info.IsDir() {
		kind = "文件夹"
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = "快捷方式"
	}
	text := fmt.Sprintf("%s\n类型: %s\n大小: %d\n修改时间: %s\n权限: %s\n",
		path, kind, info.Size(), info.ModTime().Format("2006-01-02 15:04"), info.Mode())
	return mcpText(text), nil, nil
}

func (s *sunvaleTools) mkdir(_ context.Context, _ *mcp.CallToolRequest, args nameArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	name := strings.TrimSpace(args.Name)
	if name == "" {
		var err error
		name, err = uniqueNewFolderName(root)
		if err != nil {
			return mcpErr(err), nil, nil
		}
	}
	if msg := rename.ValidateName(name); msg != "" {
		return mcpErr(fmt.Errorf("%s", msg)), nil, nil
	}
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		return mcpErr(err), nil, nil
	}
	s.noteUndo(undoOp{Desc: "新建文件夹", Created: []string{path}})
	return mcpText("已新建文件夹 " + path), nil, nil
}

func (s *sunvaleTools) newTextFile(_ context.Context, _ *mcp.CallToolRequest, args nameArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	name := strings.TrimSpace(args.Name)
	if name == "" {
		var err error
		name, err = uniqueFreeName(root, "新建文本文档.txt", false)
		if err != nil {
			return mcpErr(err), nil, nil
		}
	}
	if msg := rename.ValidateName(name); msg != "" {
		return mcpErr(fmt.Errorf("%s", msg)), nil, nil
	}
	path := filepath.Join(root, name)
	if _, err := os.Lstat(path); err == nil {
		return mcpErr(fmt.Errorf("已经存在")), nil, nil
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return mcpErr(err), nil, nil
	}
	s.noteUndo(undoOp{Desc: "新建文本文档", Created: []string{path}})
	return mcpText("已新建 " + path), nil, nil
}

func (s *sunvaleTools) copyItems(_ context.Context, _ *mcp.CallToolRequest, args transferArgs) (*mcp.CallToolResult, any, error) {
	return s.transfer(args, false)
}

func (s *sunvaleTools) moveItems(_ context.Context, _ *mcp.CallToolRequest, args transferArgs) (*mcp.CallToolResult, any, error) {
	return s.transfer(args, true)
}

func (s *sunvaleTools) transfer(args transferArgs, move bool) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	dest, err := s.resolveDest(args.Dest)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		return mcpErr(fmt.Errorf("目标不是文件夹")), nil, nil
	}
	var created []string
	var moved []undoMove
	var lines []string
	for _, name := range args.Names {
		src, err := resolveInRoot(root, name)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		if err := refuseRootish(src); err != nil {
			return mcpErr(err), nil, nil
		}
		if pathWithin(dest, src) {
			return mcpErr(fmt.Errorf("不能把文件夹放进它自己里面")), nil, nil
		}
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		dstName, err := freeName(dest, filepath.Base(src), srcInfo.IsDir())
		if err != nil {
			return mcpErr(err), nil, nil
		}
		dst := filepath.Join(dest, dstName)
		if move && filepath.Clean(filepath.Dir(src)) == filepath.Clean(dest) && dstName == filepath.Base(src) {
			lines = append(lines, src+" 已在目标中")
			continue
		}
		aiFx.markBusy(src)
		if err := placeItem(src, dst, move); err != nil {
			aiFx.clearBusy(src)
			return mcpErr(err), nil, nil
		}
		if move {
			aiFx.markLeaving(src)
			moved = append(moved, undoMove{From: src, To: dst})
			lines = append(lines, "已移动到 "+dst)
		} else {
			created = append(created, dst)
			lines = append(lines, "已复制到 "+dst)
		}
	}
	if len(created)+len(moved) == 0 {
		return mcpErr(fmt.Errorf("没有要处理的项目")), nil, nil
	}
	desc := "复制"
	if move {
		desc = "移动"
	}
	s.noteUndo(undoOp{Desc: desc, Created: created, Moved: moved})
	return mcpText(strings.Join(lines, "\n")), nil, nil
}

func (s *sunvaleTools) trashItems(_ context.Context, _ *mcp.CallToolRequest, args namesArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	var moved []undoMove
	var lines []string
	for _, name := range args.Names {
		path, err := resolveInRoot(root, name)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		if err := refuseRootish(path); err != nil {
			return mcpErr(err), nil, nil
		}
		aiFx.markBusy(path)
		dst, _, err := moveToTrash(path)
		if err != nil {
			aiFx.clearBusy(path)
			return mcpErr(err), nil, nil
		}
		aiFx.markLeaving(path)
		if dst != "" {
			moved = append(moved, undoMove{From: path, To: dst})
		}
		lines = append(lines, "已移到回收站 "+path)
	}
	if len(moved) == 0 {
		return mcpErr(fmt.Errorf("没有要删除的项目")), nil, nil
	}
	s.noteUndo(undoOp{Desc: "删除", Moved: moved})
	return mcpText(strings.Join(lines, "\n")), nil, nil
}

func (s *sunvaleTools) renameItem(_ context.Context, _ *mcp.CallToolRequest, args renameOneArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	if msg := rename.ValidateName(args.To); msg != "" {
		return mcpErr(fmt.Errorf("%s", msg)), nil, nil
	}
	src, err := resolveInRoot(root, args.Name)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if filepath.Clean(src) == filepath.Clean(root) {
		return mcpErr(fmt.Errorf("不能重命名当前文件夹本身")), nil, nil
	}
	dst := filepath.Join(root, strings.TrimSpace(args.To))
	aiFx.markBusy(src)
	if err := os.Rename(src, dst); err != nil {
		aiFx.clearBusy(src)
		return mcpErr(err), nil, nil
	}
	aiFx.markRenamed(src, dst)
	s.noteUndo(undoOp{Desc: "重命名", Moved: []undoMove{{From: src, To: dst}}})
	return mcpText("已重命名为 " + dst), nil, nil
}

func (s *sunvaleTools) openItem(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if err := sysOpen(path); err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText("已打开 " + path), nil, nil
}

func (s *sunvaleTools) openTerminal(_ context.Context, _ *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, any, error) {
	path, err := s.resolveEither(args.Path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if !info.IsDir() {
		path = filepath.Dir(path)
	}
	if err := sysTerminal(path); err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText("已在终端打开 " + path), nil, nil
}

func (s *sunvaleTools) copyAsPath(_ context.Context, _ *mcp.CallToolRequest, args namesArgs) (*mcp.CallToolResult, any, error) {
	root, _ := s.bound()
	var paths []string
	for _, name := range args.Names {
		path, err := resolveInRoot(root, name)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return mcpErr(fmt.Errorf("没有路径")), nil, nil
	}
	text := strings.Join(paths, "\n")
	_ = copyText(text)
	return mcpText(text), nil, nil
}

func (s *sunvaleTools) resolveEither(p string) (string, error) {
	root, _ := s.bound()
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return root, nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return resolveInRoot(root, p)
}

func (s *sunvaleTools) resolveDest(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		root, _ := s.bound()
		return root, nil
	}
	return s.resolveEither(p)
}

func listDirText(dir string, hidden bool) (*mcp.CallToolResult, any, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	var b strings.Builder
	n := 0
	for _, e := range ents {
		name := e.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if n >= 200 {
			b.WriteString("…已截断\n")
			break
		}
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
		n++
	}
	if n == 0 {
		return mcpText("（空）"), nil, nil
	}
	return mcpText(b.String()), nil, nil
}

func refuseRootish(path string) error {
	p := filepath.Clean(path)
	if p == "" || p == "." || p == "/" || specialPath(p) || filepath.Dir(p) == p {
		return fmt.Errorf("不能对这个位置做修改")
	}
	return nil
}
