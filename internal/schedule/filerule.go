package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sunvale/internal/fsops"
)

// PlannedOp is one file the rule would touch. Dry-run builds these;
// ApplyFileRule executes them.
type PlannedOp struct {
	Src string
	Dst string
	Op  string // move|copy
}

// MatchName reports whether a base name matches the rule's Match field.
// Empty or "*" matches everything. Comma-separated parts may be:
//   - an extension with leading dot (".pdf")
//   - a bare extension ("pdf")
//   - a filepath.Match glob ("*.docx", "报告*")
func MatchName(name, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	base := filepath.Base(name)
	lower := strings.ToLower(base)
	for _, part := range strings.Split(pattern, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "*" {
			return true
		}
		if strings.HasPrefix(part, ".") {
			if strings.HasSuffix(lower, strings.ToLower(part)) {
				return true
			}
			continue
		}
		if !strings.ContainsAny(part, "*?[") && !strings.Contains(part, ".") {
			// bare extension: pdf
			if strings.HasSuffix(lower, "."+strings.ToLower(part)) {
				return true
			}
			continue
		}
		ok, err := filepath.Match(strings.ToLower(part), lower)
		if err == nil && ok {
			return true
		}
		// Also try original case for globs that use case-sensitive Match.
		ok, err = filepath.Match(part, base)
		if err == nil && ok {
			return true
		}
	}
	return false
}

// PlanFileRule lists matching entries in SourceDir (non-recursive) and
// computes destinations. Directories match only when the pattern is empty
// or explicitly includes them via glob; extension patterns skip dirs.
// dateStamp places files under DestDir/YYYY-MM-DD.
func PlanFileRule(j Job, now time.Time) ([]PlannedOp, error) {
	src := strings.TrimSpace(j.SourceDir)
	dstRoot := strings.TrimSpace(j.DestDir)
	if src == "" || dstRoot == "" {
		return nil, fmt.Errorf("源目录和目标目录不能为空")
	}
	src = filepath.Clean(src)
	dstRoot = filepath.Clean(dstRoot)
	if j.Op == OpMove && fsops.PathWithin(dstRoot, src) && dstRoot != src {
		return nil, fmt.Errorf("目标目录不能位于源目录内")
	}
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("源目录：%w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("源路径不是目录")
	}
	destDir := dstRoot
	if j.DateStamp {
		destDir = filepath.Join(dstRoot, now.Format("2006-01-02"))
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	op := j.Op
	if op != OpMove && op != OpCopy {
		op = OpMove
	}
	var out []PlannedOp
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if e.IsDir() {
			// Extension-style patterns never match directories.
			if looksLikeExtPattern(j.Match) {
				continue
			}
			if !MatchName(name, j.Match) {
				continue
			}
		} else if !MatchName(name, j.Match) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(destDir, name)
		if filepath.Clean(srcPath) == filepath.Clean(dstPath) {
			continue
		}
		out = append(out, PlannedOp{Src: srcPath, Dst: dstPath, Op: op})
	}
	return out, nil
}

func looksLikeExtPattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return false
	}
	for _, part := range strings.Split(pattern, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.ContainsAny(part, "*?[") {
			return false
		}
		if strings.HasPrefix(part, ".") || !strings.Contains(part, ".") {
			// .pdf or pdf
			continue
		}
		return false
	}
	return true
}

// ApplyFileRule runs a plan. Dest parent dirs are created. Name collisions
// pick a unique sibling via fsops.UniqueCopyName. Returns how many succeeded
// and a joined error of failures (partial success is allowed).
func ApplyFileRule(ops []PlannedOp) (ok int, err error) {
	var failed []string
	for _, p := range ops {
		if err := applyOne(p); err != nil {
			failed = append(failed, filepath.Base(p.Src)+": "+err.Error())
			continue
		}
		ok++
	}
	if len(failed) > 0 {
		return ok, fmt.Errorf("%s", strings.Join(failed, "; "))
	}
	return ok, nil
}

func applyOne(p PlannedOp) error {
	if err := os.MkdirAll(filepath.Dir(p.Dst), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(p.Src)
	if err != nil {
		return err
	}
	dst := p.Dst
	if _, err := os.Lstat(dst); err == nil {
		name, uerr := fsops.UniqueCopyName(filepath.Dir(dst), filepath.Base(dst), info.IsDir())
		if uerr != nil {
			return uerr
		}
		dst = filepath.Join(filepath.Dir(dst), name)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	switch p.Op {
	case OpCopy:
		if info.IsDir() {
			return fsops.CopyDir(p.Src, dst)
		}
		return fsops.CopyFile(p.Src, dst, info)
	default: // move
		if err := os.Rename(p.Src, dst); err == nil {
			return nil
		}
		// Cross-device: copy then remove.
		if info.IsDir() {
			if err := fsops.CopyDir(p.Src, dst); err != nil {
				return err
			}
		} else {
			if err := fsops.CopyFile(p.Src, dst, info); err != nil {
				return err
			}
		}
		return os.RemoveAll(p.Src)
	}
}
