package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
)

// shellcore_write.go implements write and text-processing builtins for the
// in-app shell (sed/cp/mv/mkdir/rm/touch/stat/du/sort/uniq/cut/tr/basename/
// dirname/date/seq/env/file). Split from shellcore.go (LAY1).

func (s *shellCore) cmdSed(flags map[string]string, rest []string) {
	// sed 's/old/new/' [文件…] — a pipe's sed reads stdin; with a file it
	// writes the result there with -i, else to stdout.
	inPlace := flags["i"] != ""
	if len(rest) == 0 {
		s.errln("sed: 用法 sed 's/old/new/' [文件…]")
		return
	}
	script := rest[0]
	if !strings.HasPrefix(script, "s") {
		s.errln("sed: 只支持 s/// 替换")
		return
	}
	parts := strings.SplitN(script[2:], "/", 3)
	if len(parts) < 2 {
		s.errln("sed: 表达式不对")
		return
	}
	from, to := parts[0], parts[1]
	re, err := regexp.Compile(from)
	if err != nil {
		s.errln("sed: 表达式不对：%v", err)
		return
	}
	global := len(parts) > 2 && strings.Contains(parts[2], "g")
	apply := func(body string) string {
		done := 0
		return re.ReplaceAllStringFunc(body, func(m string) string {
			done++
			if !global && done > 1 {
				return m
			}
			return re.ReplaceAllString(m, to)
		})
	}
	files := rest[1:]
	if len(files) == 0 {
		b, _ := io.ReadAll(s.in)
		s.out.Write([]byte(apply(string(b))))
		return
	}
	for _, file := range files {
		abs, ok := s.check(file)
		if !ok {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			s.errln("sed: %s: 打不开", file)
			continue
		}
		out := apply(string(data))
		if inPlace {
			if err := os.WriteFile(abs, []byte(out), 0o644); err != nil {
				s.errln("sed: %s: 写不了", file)
			}
			continue
		}
		s.out.Write([]byte(out))
	}
}

func (s *shellCore) cmdCp(flags map[string]string, rest []string) {
	if len(rest) < 2 {
		s.errln("cp: 用法 cp 源… 目标")
		return
	}
	dstAbs, ok := s.check(rest[len(rest)-1])
	if !ok {
		return
	}
	dst := dstAbs
	for _, p := range rest[:len(rest)-1] {
		src, ok := s.check(p)
		if !ok {
			continue
		}
		target := dst
		if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
			target = filepath.Join(dst, filepath.Base(src))
		}
		if err := copyPathShell(src, target, flags["r"] != "" || flags["recursive"] != ""); err != nil {
			s.errln("cp: %s: %v", p, err)
		}
	}
}

func (s *shellCore) cmdMv(flags map[string]string, rest []string) {
	if len(rest) < 2 {
		s.errln("mv: 用法 mv 源… 目标")
		return
	}
	dstAbs, ok := s.check(rest[len(rest)-1])
	if !ok {
		return
	}
	dst := dstAbs
	for _, p := range rest[:len(rest)-1] {
		src, ok := s.check(p)
		if !ok {
			continue
		}
		target := dst
		if fi, err := os.Stat(dst); err == nil && fi.IsDir() {
			target = filepath.Join(dst, filepath.Base(src))
		}
		if err := os.Rename(src, target); err != nil {
			if err := copyPathShell(src, target, true); err != nil {
				s.errln("mv: %s: %v", p, err)
				continue
			}
			_ = os.RemoveAll(src)
		}
	}
}

func (s *shellCore) cmdMkdir(flags map[string]string, rest []string) {
	if len(rest) == 0 {
		return
	}
	first, ok := s.check(rest[0])
	if !ok {
		return
	}
	if err := os.MkdirAll(first, 0o755); err != nil {
		s.errln("mkdir: %v", err)
	}
	for _, p := range rest[1:] {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		os.MkdirAll(abs, 0o755)
	}
}

func (s *shellCore) cmdRm(flags map[string]string, rest []string) {
	recursive := flags["r"] != "" || flags["recursive"] != "" || flags["rf"] != ""
	for _, p := range rest {
		if strings.HasPrefix(p, "-") {
			continue
		}
		target, ok := s.check(p)
		if !ok {
			continue
		}
		if fi, err := os.Lstat(target); err == nil && fi.IsDir() {
			if !recursive {
				s.errln("rm: %s 是文件夹，加 -r", p)
				continue
			}
			os.RemoveAll(target)
			continue
		}
		if err := os.Remove(target); err != nil {
			s.errln("rm: %s: %v", p, err)
		}
	}
}

func (s *shellCore) cmdTouch(rest []string) {
	for _, p := range rest {
		target, ok := s.check(p)
		if !ok {
			continue
		}
		now := time.Now()
		if _, err := os.Stat(target); err == nil {
			os.Chtimes(target, now, now)
			continue
		}
		if f, err := os.Create(target); err == nil {
			f.Close()
		}
	}
}

func (s *shellCore) cmdStat(rest []string) {
	for _, p := range rest {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		info, err := os.Lstat(abs)
		if err != nil {
			s.errln("stat: %s: 没有", p)
			continue
		}
		s.outln("文件：%s", p)
		s.outln("大小：%d", info.Size())
		s.outln("修改：%s", info.ModTime().Format("2006-01-02 15:04:05"))
		s.outln("类型：%s", modeKindShell(info.Mode()))
	}
}

func (s *shellCore) cmdDu(rest []string) {
	root := "."
	if len(rest) > 0 {
		root = rest[0]
	}
	var total int64
	base, ok := s.check(root)
	if !ok {
		return
	}
	filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	s.outln("%d\t%s", total, root)
}

func (s *shellCore) cmdSort(flags map[string]string, rest []string) {
	var lines []string
	if len(rest) == 0 {
		sc := bufio.NewScanner(s.in)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	} else {
		for _, p := range rest {
			data, err := os.ReadFile(s.resolve(p))
			if err != nil {
				s.errln("sort: %s: 打不开", p)
				continue
			}
			lines = append(lines, strings.Split(strings.TrimRight(string(data), "\n"), "\n")...)
		}
	}
	sort.Strings(lines)
	if flags["u"] != "" || flags["unique"] != "" {
		var out []string
		seen := map[string]bool{}
		for _, l := range lines {
			if seen[l] {
				continue
			}
			seen[l] = true
			out = append(out, l)
		}
		lines = out
	}
	for _, l := range lines {
		s.outln("%s", l)
	}
}

func (s *shellCore) cmdUniq(rest []string) {
	var lines []string
	if len(rest) == 0 {
		sc := bufio.NewScanner(s.in)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	} else {
		data, _ := os.ReadFile(s.resolve(rest[0]))
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	for i, l := range lines {
		if i > 0 && l == lines[i-1] {
			continue
		}
		s.outln("%s", l)
	}
}

func (s *shellCore) cmdCut(flags map[string]string, rest []string) {
	field := 1
	if v := flags["f"]; v != "" {
		fmt.Sscanf(v, "%d", &field)
	}
	delim := "\t"
	if v := flags["d"]; v != "" {
		delim = v
	}
	var lines []string
	if len(rest) == 0 {
		sc := bufio.NewScanner(s.in)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
	} else {
		data, _ := os.ReadFile(s.resolve(rest[0]))
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	for _, l := range lines {
		parts := strings.Split(l, delim)
		if field <= len(parts) {
			s.outln("%s", parts[field-1])
		}
	}
}

func (s *shellCore) cmdTr(flags map[string]string, rest []string) {
	if len(rest) < 2 {
		return
	}
	from, to := strings.Trim(rest[0], "'\""), strings.Trim(rest[1], "'\"")
	repl := strings.NewReplacer(from, to)
	sc := bufio.NewScanner(s.in)
	for sc.Scan() {
		s.outln("%s", repl.Replace(sc.Text()))
	}
}

func (s *shellCore) cmdBasename(rest []string) {
	for _, p := range rest {
		s.outln("%s", filepath.Base(strings.TrimSuffix(p, "/")))
	}
}

func (s *shellCore) cmdDirname(rest []string) {
	for _, p := range rest {
		s.outln("%s", filepath.Dir(strings.TrimSuffix(p, "/")))
	}
}

func (s *shellCore) cmdDate(rest []string) {
	s.outln("%s", time.Now().Format("2006-01-02 15:04:05"))
}

func (s *shellCore) cmdSeq(rest []string) {
	if len(rest) == 0 {
		return
	}
	start, end, step := 1, 0, 1
	switch len(rest) {
	case 1:
		fmt.Sscanf(rest[0], "%d", &end)
	case 2:
		fmt.Sscanf(rest[0], "%d", &start)
		fmt.Sscanf(rest[1], "%d", &end)
	case 3:
		fmt.Sscanf(rest[2], "%d", &step)
	}
	if step == 0 {
		step = 1
	}
	for i := start; (step > 0 && i <= end) || (step < 0 && i >= end); i += step {
		s.outln("%d", i)
	}
}

func (s *shellCore) cmdEnv(rest []string) {
	s.env.Each(func(name string, vr expand.Variable) bool {
		s.outln("%s=%s", name, vr.String())
		return true
	})
}

func (s *shellCore) cmdFileInfo(rest []string) {
	for _, p := range rest {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		info, err := os.Lstat(abs)
		if err != nil {
			s.errln("fileinfo: %s: 没有", p)
			continue
		}
		s.outln("%s\t%s\t%d\t%s", p, modeKindShell(info.Mode()), info.Size(),
			info.ModTime().Format("2006-01-02 15:04"))
	}
}

func modeKindShell(m os.FileMode) string {
	if m.IsDir() {
		return "文件夹"
	}
	if m&os.ModeSymlink != 0 {
		return "链接"
	}
	return "文件"
}

// copyPathShell copies file or tree; recursive is required for a folder.
func copyPathShell(src, dst string, recursive bool) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return copyFileShell(src, dst)
	}
	if !recursive {
		return fmt.Errorf("%s 是文件夹，加 -r", filepath.Base(src))
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFileShell(p, target)
	})
}

func copyFileShell(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
