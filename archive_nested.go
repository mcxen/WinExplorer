package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// archive_nested.go is the recursive half of the password book: after the
// top archive opens, archives found inside open the same way — the book is
// re-tried for each, and whatever password won moves to the front, since a
// set of nested archives usually shares one. A nested archive no candidate
// opens is not a failure of the whole run: the rest of the tree still
// lands, and the report names which inner archives stayed locked so the
// caller can ask for one more secret instead of redoing everything.
//
// Nesting is bounded: archives that contain themselves (or just huge
// trees) stop at nestedMaxDepth levels and nestedMaxCount inner archives.

const (
	nestedMaxDepth = 6
	nestedMaxCount = 32
)

// extractWithPasswordsFn is the one-archive engine the walker calls. It is
// a variable so the nesting tests can drive password scenarios without a
// 7-Zip on the machine.
var extractWithPasswordsFn = extractWithPasswords

// nestedReport is what the walk beyond the top archive did.
type nestedReport struct {
	// Extracted is each inner archive that opened, path relative to the
	// top destination, in opening order.
	Extracted []string
	// Locked is inner archives no password candidate opened.
	Locked []string
	// Failed is inner archives that failed for another reason, followed by
	// the error's short form.
	Failed []string
}

// any reports whether the walk found inner archives at all.
func (r nestedReport) any() bool {
	return len(r.Extracted)+len(r.Locked)+len(r.Failed) > 0
}

// extractArchiveNested opens archive into dstDir with the password book,
// then opens every archive inside, recursively. The top password is
// returned as extractWithPasswords reports it; the walk's part never fails
// the call — its outcome is the report.
func extractArchiveNested(archive, dstDir string, passwords []string) (string, nestedReport, error) {
	var r nestedReport
	password, err := extractWithPasswordsFn(archive, dstDir, passwords)
	if err != nil {
		return "", r, err
	}
	// The winning password goes first for the inner archives, then the
	// plain try, then the rest of the book in the pane's order.
	ordered := reprioritize(password, passwords)
	seen := map[string]bool{filepath.Clean(archive): true}
	queue := []string{dstDir}
	opened := 0
	for depth := 0; len(queue) > 0 && depth < nestedMaxDepth && opened < nestedMaxCount; depth++ {
		if archiveTaskFor(archive).isCanceled() {
			return "", r, errArchiveCanceled
		}
		var next []string
		for _, dir := range queue {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				path := filepath.Join(dir, e.Name())
				if e.IsDir() {
					next = append(next, path)
					continue
				}
				if !isArchive(e.Name()) || seen[filepath.Clean(path)] {
					continue
				}
				seen[filepath.Clean(path)] = true
				opened++
				if opened > nestedMaxCount {
					break
				}
				rel := nestedRel(dstDir, path)
				sub, err := uniqueFreeName(dir, archiveExtName(e.Name()), true)
				if err != nil {
					r.Failed = append(r.Failed, rel+"："+err.Error())
					continue
				}
				sub = filepath.Join(dir, sub)
				pwd, err := extractWithPasswordsFn(path, sub, ordered)
				if err != nil {
					_ = os.RemoveAll(sub)
					if errors.Is(err, errArchivePassword) {
						r.Locked = append(r.Locked, rel)
					} else {
						r.Failed = append(r.Failed, rel+"："+err.Error())
					}
					continue
				}
				if pwd != "" {
					ordered = reprioritize(pwd, ordered)
				}
				r.Extracted = append(r.Extracted, rel)
				// The fresh folder may hold the next level down.
				next = append(next, sub)
			}
		}
		queue = next
	}
	return password, r, nil
}

// reprioritize moves winner to the front of candidates ("" first when no
// winner) without duplicating it.
func reprioritize(winner string, candidates []string) []string {
	out := []string{winner}
	for _, p := range candidates {
		if p != winner {
			out = append(out, p)
		}
	}
	return out
}

// nestedRel is path's name inside the report: relative to the top
// destination when it is under it, plain otherwise.
func nestedRel(dstDir, path string) string {
	if rel, err := filepath.Rel(dstDir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

// nestedSummary is the tools' and the status bar's one-line readout of a
// walk: "" when nothing nested was found.
func (r nestedReport) summary() string {
	var parts []string
	if n := len(r.Extracted); n > 0 {
		parts = append(parts, "自动解开 "+itoa(n)+" 个内层归档")
	}
	if n := len(r.Locked); n > 0 {
		names := r.Locked
		if len(names) > 3 {
			names = names[:3]
		}
		parts = append(parts, itoa(n)+" 个内层归档密码本没解开（"+strings.Join(names, "、")+"）")
	}
	if n := len(r.Failed); n > 0 {
		parts = append(parts, itoa(n)+" 个内层归档解压失败")
	}
	return strings.Join(parts, "；")
}
