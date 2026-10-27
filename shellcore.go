package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
)

// shellcore.go is the app's own shell: mvdan.cc/sh plus Go coreutils for
// platforms without ls/grep. This file keeps the interpreter wiring and the
// read-oriented commands (ls/cat/head/tail/grep/wc/find). Write and text
// utilities live in shellcore_write.go (LAY1 split).

type shellCore struct {
	dir string
	env expand.Environ
	in  io.Reader
	out io.Writer
	err io.Writer
	ctx context.Context
}

// builtinCommands are the Go coreutils. A command on this list is never
// looked up on PATH: the builtin answers, the same on every platform, so
// one script runs on macOS, Linux and Windows alike.
var builtinCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "grep": true,
	"wc": true, "find": true, "sed": true, "cp": true, "mv": true,
	"mkdir": true, "rm": true, "touch": true, "stat": true, "du": true,
	"sort": true, "uniq": true, "cut": true, "tr": true,
	"basename": true, "dirname": true, "date": true, "seq": true,
	"env": true, "fileinfo": true,
}

// shellExec runs args, preferring a real binary on PATH on Unix and
// falling back to the Go coreutils. Windows is the reverse: a builtin
// answers first, because a stray GnuWin32 ls behind the model's back
// would make the same script behave two ways.
func shellExec(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return nil
		}
		if !builtinCommands[args[0]] {
			if runtime.GOOS != "windows" {
				return next(ctx, args)
			}
			// On Windows a PATH binary still wins when it is really
			// there: git-bash users expect their own tooling.
			if _, err := exec.LookPath(args[0]); err != nil {
				return coreErr(args[0])
			}
			return next(ctx, args)
		}
		core := shellCoreFrom(ctx)
		core.run(args)
		return nil
	}
}

func coreErr(name string) error {
	return fmt.Errorf("%s：内置 shell 不提供这个命令", name)
}

func shellCoreFrom(ctx context.Context) *shellCore {
	hctx := interp.HandlerCtx(ctx)
	return &shellCore{
		dir: hctx.Dir, env: hctx.Env,
		in: hctx.Stdin, out: hctx.Stdout, err: hctx.Stderr, ctx: ctx,
	}
}

// markWrites lights the rows a mutating command touches: the same
// breathing light the file tools and the unpacker give the listing, so the
// user sees which files the shell is working on. mv's sources move away,
// so only the destination keeps the light.
func (s *shellCore) markWrites(rest []string, skipSources bool) {
	if len(rest) == 0 {
		return
	}
	lit := rest
	if skipSources {
		lit = rest[len(rest)-1:]
	}
	for _, p := range lit {
		if strings.HasPrefix(p, "-") {
			continue
		}
		if abs, ok := s.check(p); ok {
			aiFx.markBusy(abs)
		}
	}
}

// run dispatches one builtin command with its flags.
func (s *shellCore) run(args []string) {
	name := args[0]
	valued := ""
	switch name {
	case "head", "tail", "sort":
		valued = "n"
	case "cut":
		valued = "fd"
	}
	flags, rest := parseArgs(args[1:], valued)
	inPlace := flags["i"] != ""
	if name == "find" {
		// find parses its own -name/-type values; the generic flag
		// reader would eat them as booleans.
		flags, rest = map[string]string{}, args[1:]
	}
	switch name {
	case "ls":
		s.cmdLs(flags, rest)
	case "cat":
		s.cmdCat(rest)
	case "head":
		s.cmdHead(flags, rest)
	case "tail":
		s.cmdTail(flags, rest)
	case "grep":
		s.cmdGrep(flags, rest)
	case "wc":
		s.cmdWc(flags, rest)
	case "find":
		s.cmdFind(rest)
	case "sed":
		if inPlace {
			s.markWrites(rest, false)
		}
		s.cmdSed(flags, rest)
	case "cp":
		s.markWrites(rest, true)
		s.cmdCp(flags, rest)
	case "mv":
		s.markWrites(rest, true)
		s.cmdMv(flags, rest)
	case "mkdir":
		s.markWrites(rest, false)
		s.cmdMkdir(flags, rest)
	case "rm":
		s.markWrites(rest, false)
		s.cmdRm(flags, rest)
	case "touch":
		s.markWrites(rest, false)
		s.cmdTouch(rest)
	case "stat":
		s.cmdStat(rest)
	case "du":
		s.cmdDu(rest)
	case "sort":
		s.cmdSort(flags, rest)
	case "uniq":
		s.cmdUniq(rest)
	case "cut":
		s.cmdCut(flags, rest)
	case "tr":
		s.cmdTr(flags, rest)
	case "basename":
		s.cmdBasename(rest)
	case "dirname":
		s.cmdDirname(rest)
	case "date":
		s.cmdDate(rest)
	case "seq":
		s.cmdSeq(rest)
	case "env":
		s.cmdEnv(rest)
	case "fileinfo":
		s.cmdFileInfo(rest)
	default:
		fmt.Fprintf(s.err, "%s: 不支持的内置命令\n", args[0])
	}
}

func (s *shellCore) resolve(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(s.dir, p))
}

// check refuses a path outside the run's root: the interpreter's open
// handler guards redirections, this guards the coreutils' own opens.
// /dev/null passes through like the open handler's own pass: dropping
// output is hygiene, not an escape.
func (s *shellCore) check(p string) (string, bool) {
	if p == "/dev/null" {
		return p, true
	}
	abs := s.resolve(p)
	if abs != s.dir && !strings.HasPrefix(abs, s.dir+string(filepath.Separator)) {
		s.errln("%s 在工作目录之外，已拒绝", p)
		return "", false
	}
	return abs, true
}

func (s *shellCore) outln(format string, a ...any) {
	fmt.Fprintf(s.out, format+"\n", a...)
}

func (s *shellCore) errln(format string, a ...any) {
	fmt.Fprintf(s.err, format+"\n", a...)
}

// parseArgs splits argv after the command into flags (-l, --all, bundled
// -la) and operands. Flags that take a value — -n 3, -d, -f — get it from
// the same bundle (-n3) or the next argv (opts).
func parseArgs(args []string, valued string) (map[string]string, []string) {
	flags := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			flags[a[2:]] = ""
			continue
		}
		if len(a) > 1 && a[0] == '-' {
			letters := a[1:]
			for j := 0; j < len(letters); j++ {
				key := string(letters[j])
				if strings.Contains(valued, key) {
					// -n3 takes the tail; -n 3 takes the next argv.
					if j+1 < len(letters) {
						flags[key] = letters[j+1:]
					} else if i+1 < len(args) {
						i++
						flags[key] = args[i]
					}
					continue
				}
				flags[key] = ""
			}
			continue
		}
		rest = append(rest, a)
	}
	return flags, rest
}

// — the commands —

func (s *shellCore) cmdLs(flags map[string]string, rest []string) {
	long := flags["l"] != ""
	all := flags["a"] != "" || flags["all"] != ""
	print := func(p, name string, e os.DirEntry) {
		if !long {
			s.outln("%s", name)
			return
		}
		inf, err := e.Info()
		if err != nil {
			return
		}
		kind := "-"
		if e.IsDir() {
			kind = "d"
		}
		s.outln("%s %8d %s %s", kind, inf.Size(),
			inf.ModTime().Format("2006-01-02 15:04"), name)
	}
	// No operand: the directory itself. File operands print as they are,
	// as ls does; one directory operand lists it.
	if len(rest) == 0 {
		s.listDir(s.dir, ".", long, all, print)
		return
	}
	for _, target := range rest {
		p := s.resolve(target)
		info, err := os.Stat(p)
		if err != nil {
			s.errln("ls: %s: 没有这个文件或文件夹", target)
			continue
		}
		if !info.IsDir() {
			print(p, target, statEntry(p))
			continue
		}
		if len(rest) > 1 {
			s.outln("%s:", target)
		}
		s.listDir(p, target, long, all, print)
	}
}

// listDir prints one directory's entries, hidden ones only with -a.
func (s *shellCore) listDir(dir, shown string, long, all bool, print func(p, name string, e os.DirEntry)) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		s.errln("ls: %s: 读不了", shown)
		return
	}
	sort.Slice(ents, func(i, j int) bool { return strings.ToLower(ents[i].Name()) < strings.ToLower(ents[j].Name()) })
	for _, e := range ents {
		name := e.Name()
		if !all && strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		print(filepath.Join(dir, e.Name()), name, e)
	}
}

// statEntry re-wraps an already-stat'ed path as a DirEntry for the
// file-operand case, so both paths print through one helper.
func statEntry(p string) os.DirEntry {
	info, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	return statDirEntry{info}
}

type statDirEntry struct{ os.FileInfo }

func (d statDirEntry) Type() os.FileMode          { return d.FileInfo.Mode().Type() }
func (d statDirEntry) Info() (os.FileInfo, error) { return d.FileInfo, nil }

func (s *shellCore) cmdCat(rest []string) {
	if len(rest) == 0 {
		io.Copy(s.out, s.in)
		return
	}
	for _, p := range rest {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		f, err := os.Open(abs)
		if err != nil {
			s.errln("cat: %s: 打不开", p)
			continue
		}
		io.Copy(s.out, f)
		f.Close()
		s.out.Write([]byte{'\n'})
	}
}

func (s *shellCore) cmdHead(flags map[string]string, rest []string) {
	n := 10
	if v := flags["n"]; v != "" {
		fmt.Sscanf(v, "%d", &n)
	}
	if len(rest) == 0 {
		sc := bufio.NewScanner(s.in)
		line := 0
		for sc.Scan() && line < n {
			s.outln("%s", sc.Text())
			line++
		}
		return
	}
	for _, p := range rest {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		f, err := os.Open(abs)
		if err != nil {
			s.errln("head: %s: 打不开", p)
			continue
		}
		sc := bufio.NewScanner(f)
		line := 0
		for sc.Scan() && line < n {
			s.outln("%s", sc.Text())
			line++
		}
		f.Close()
	}
}

func (s *shellCore) cmdTail(flags map[string]string, rest []string) {
	n := 10
	if v := flags["n"]; v != "" {
		fmt.Sscanf(v, "%d", &n)
	}
	if len(rest) == 0 {
		sc := bufio.NewScanner(s.in)
		var lines []string
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, l := range lines {
			s.outln("%s", l)
		}
		return
	}
	for _, p := range rest {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			s.errln("tail: %s: 打不开", p)
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, l := range lines {
			s.outln("%s", l)
		}
	}
}

func (s *shellCore) cmdGrep(flags map[string]string, rest []string) {
	if len(rest) == 0 {
		return
	}
	pattern := rest[0]
	re, err := regexp.Compile(pattern)
	if err != nil {
		s.errln("grep: 表达式不对：%v", err)
		return
	}
	countOnly := flags["c"] != ""
	ignoreCase := flags["i"] != "" || flags["ignore-case"] != ""
	if ignoreCase {
		re = regexp.MustCompile("(?i)" + pattern)
	}
	files := rest[1:]
	// A piped grep (no file operand) reads stdin.
	if len(files) == 0 {
		sc := bufio.NewScanner(s.in)
		hits := 0
		for sc.Scan() {
			if re.MatchString(sc.Text()) {
				hits++
				if !countOnly {
					s.outln("%s", sc.Text())
				}
			}
		}
		if countOnly {
			s.outln("%d", hits)
		}
		return
	}
	for _, p := range files {
		abs, ok := s.check(p)
		if !ok {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			s.errln("grep: %s: 打不开", p)
			continue
		}
		hits := 0
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if re.MatchString(l) {
				hits++
				if !countOnly {
					s.outln("%s", l)
				}
			}
		}
		if countOnly {
			s.outln("%d", hits)
		}
	}
}

func (s *shellCore) cmdWc(flags map[string]string, rest []string) {
	countLines := func(b []byte) (int, int, int) {
		lines := strings.Count(string(b), "\n")
		words := len(strings.Fields(string(b)))
		return lines, words, len(b)
	}
	total := [3]int{}
	if len(rest) == 0 {
		b, _ := io.ReadAll(s.in)
		l, w, c := countLines(b)
		total[0], total[1], total[2] = l, w, c
		s.outln("%d %d %d", l, w, c)
		return
	}
	for _, p := range rest {
		b, err := os.ReadFile(s.resolve(p))
		if err != nil {
			s.errln("wc: %s: 打不开", p)
			continue
		}
		l, w, c := countLines(b)
		total[0] += l
		total[1] += w
		total[2] += c
		s.outln("%d %d %d %s", l, w, c, p)
	}
	if len(rest) > 1 {
		s.outln("%d %d %d 合计", total[0], total[1], total[2])
	}
}

func (s *shellCore) cmdFind(rest []string) {
	// find . -name "*.go" walks with a name glob; -type f keeps files
	// only, -type d folders only.
	root := "."
	name := ""
	typeOnly := ' '
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-name":
			if i+1 < len(rest) {
				name = rest[i+1]
				i++
			}
		case "-type":
			if i+1 < len(rest) {
				switch rest[i+1] {
				case "f", "file":
					typeOnly = 'f'
				case "d", "directory":
					typeOnly = 'd'
				}
				i++
			}
		default:
			if !strings.HasPrefix(rest[i], "-") {
				root = rest[i]
			}
		}
	}
	if name != "" {
		name = strings.Trim(name, "\"'")
	}
	base, ok := s.check(root)
	if !ok {
		return
	}
	filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if s.ctx.Err() != nil {
			return filepath.SkipAll
		}
		if typeOnly == 'f' && d.IsDir() {
			return nil
		}
		if typeOnly == 'd' && !d.IsDir() {
			return nil
		}
		if name != "" {
			ok, _ := filepath.Match(name, filepath.Base(p))
			if !ok {
				return nil
			}
		}
		rel, relErr := filepath.Rel(base, p)
		if relErr != nil || rel == "." {
			s.outln("%s", p)
			return nil
		}
		s.outln("%s", rel)
		return nil
	})
}
