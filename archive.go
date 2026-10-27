package main

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"sunvale/internal/fsops"
	"sunvale/internal/tasks"
)

// 归档支持：zip 与 tar 家族（含 gz/bz2）用标准库纯 Go 解压；xz 的
// tar 走系统 bsdtar，7z/rar 需要 7-Zip（7zz）。压缩只产出 ZIP，与
// Windows 资源管理器的“压缩为 ZIP 文件”一致。

// archiveExtName 返回去掉归档后缀的基础名："photos.tar.gz" → "photos"。
func archiveExtName(name string) string {
	l := strings.ToLower(name)
	for _, suf := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.bz", ".tar.lzma"} {
		if strings.HasSuffix(l, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// archiveKind 归一化归档扩展名，非归档返回 ""。
func archiveKind(name string) string {
	l := strings.ToLower(name)
	for _, suf := range []string{".tar.gz", ".tar.bz2", ".tar.xz"} {
		if strings.HasSuffix(l, suf) {
			return strings.TrimPrefix(suf, ".")
		}
	}
	switch ext := strings.TrimPrefix(filepath.Ext(l), "."); ext {
	case "zip", "tar", "tgz", "tbz2", "txz", "7z", "rar":
		return ext
	}
	return ""
}

// isArchive 报告 name 是否为受支持的归档扩展名。只看名字，不管路径是
// 文件还是目录——调用方必须自行排除目录（或用 entryIsArchive /
// isArchivePath），否则像 Foo.app 这样的包目录会被误判。
func isArchive(name string) bool { return archiveKind(name) != "" }

// doCompress 把当前选择压缩为当前文件夹中的一个 ZIP。单个项目以它
// 命名，多个项目以“压缩文件”命名，重名时按 Windows 风格加“ - 副本”。
func (a *App) doCompress() {
	sel := a.selectedEntries()
	if len(sel) == 0 || specialPath(a.cur().path) {
		return
	}
	stem := "压缩文件"
	if len(sel) == 1 {
		stem = strings.TrimSuffix(sel[0].Name, filepath.Ext(sel[0].Name))
	}
	dir := a.cur().path
	name, err := uniqueFreeName(dir, stem+".zip", false)
	if err != nil {
		a.statusMsg = "压缩失败：" + err.Error()
		return
	}
	dst := filepath.Join(dir, name)
	sel = append([]Entry(nil), sel...)
	first := true
	// The runner keeps the original selection: 重试 packs the same items
	// again, under a fresh name if the first attempt's file is in the way.
	queueArchive("compress", dst, "user", func(t *archiveTask) error {
		if !first {
			if _, err := os.Lstat(dst); err == nil {
				if n, err := uniqueFreeName(dir, name, false); err == nil {
					dst, name = filepath.Join(dir, n), n
					t.rebind(dst)
				}
			}
		}
		first = false
		t.reset(entriesSize(sel))
		err := makeZip(dst, sel)
		if err != nil {
			_ = os.Remove(dst)
		}
		dst, name := dst, name
		a.postAssist(func() {
			switch {
			case errors.Is(err, errArchiveCanceled):
				a.statusMsg = "已取消压缩 " + name
			case err != nil:
				a.statusMsg = "压缩失败：" + err.Error()
			default:
				a.reload()
				a.selectOnly(dst)
				a.statusMsg = "已创建 " + name
			}
		})
		return err
	})
}

// archiveGo runs fn off the UI thread once the app runs; headless tests
// run it inline so they see its result.
func archiveGo(fn func()) {
	if !mygo.App.IsReady() {
		fn()
		return
	}
	go fn()
}

// doExtract 解压选中的每个归档到以其基础名命名的子文件夹。加密归档
// 会按密码本顺序重试；密码本失败（或为空）时在任务卡内联询问密码
// （EXTRACT-ASK，可选写入密码本）；归档套归档时自动连解（嵌套深度与
// 个数有上限），内层同样在书失败后询问。解压是后台活：选中的行立刻
// 亮起呼吸灯，解开一个它的呼吸就换成新文件夹行的一次闪烁；全部结束
// 时列表刷新、选中新文件夹，并给出是否把原归档移入回收站的下一步推
// 荐。取消或留空则该归档失败；仍解不开的内层在状态栏点名。
func (a *App) doExtract() {
	sel := a.selectedEntries()
	if len(sel) == 0 || specialPath(a.cur().path) {
		return
	}
	a.assist.ensure()
	var archives []Entry
	for _, e := range sel {
		if !entryIsArchive(e) {
			continue
		}
		archives = append(archives, e)
	}
	if len(archives) == 0 {
		return
	}
	dir := a.cur().path
	a.ensureArchivePasswordSavedHook()
	candidates := archivePasswordCandidates(a.assist.passwords)
	if a.lastArchivePassword != "" {
		ordered := []string{a.lastArchivePassword}
		for _, p := range candidates {
			if p != a.lastArchivePassword {
				ordered = append(ordered, p)
			}
		}
		candidates = ordered
	}
	paths := make([]string, len(archives))
	for i, e := range archives {
		paths[i] = e.Path
	}
	aiFx.markBusy(paths...)
	a.statusMsg = fmt.Sprintf("正在解压 %d 个归档…", len(archives))
	go a.extractArchives(dir, archives, candidates)
}

// ensureArchivePasswordSavedHook keeps the in-memory book and last-used
// password in sync when EXTRACT-ASK saves or unlocks an archive.
func (a *App) ensureArchivePasswordSavedHook() {
	onArchivePasswordSaved = func(secret string) {
		if secret == "" {
			return
		}
		a.postAssist(func() {
			a.lastArchivePassword = secret
			for _, p := range a.assist.passwords {
				if p.Secret == secret {
					return
				}
			}
			a.assist.passwords = append(a.assist.passwords, archivePassword{
				ID: newID(), Label: "解压密码", Secret: secret,
			})
			if len(a.assist.passwords) > assistMaxArchivePasswords {
				a.assist.passwords = a.assist.passwords[len(a.assist.passwords)-assistMaxArchivePasswords:]
			}
		})
	}
}

// extractArchives runs the unpacks off the UI thread. Each archive's
// breath comes down as it lands, and the folder it produced flashes once
// the reload shows it. The window is updated in one posting at the end.
func (a *App) extractArchives(dir string, archives []Entry, candidates []string) {
	// Each archive is its own queued task (two per disk run at once); the
	// toast and selection come when the last of the batch ends. 重试 of
	// one archive reruns it alone and reports just that one. Passwords
	// the user types (EXTRACT-ASK) prepend to the shared book for later
	// archives in the same batch.
	var mu sync.Mutex
	left := len(archives)
	outs := make([]extractOut, len(archives))
	cands := append([]string(nil), candidates...)
	for i, e := range archives {
		i, e := i, e
		runs := 0
		queueArchive("extract", e.Path, "user", func(t *archiveTask) error {
			mu.Lock()
			runs++
			again := runs > 1
			book := append([]string(nil), cands...)
			mu.Unlock()
			ask := extractPasswordAsk(t)
			o := extractOne(dir, e, book, ask)
			mu.Lock()
			if o.pw != "" {
				cands = reprioritize(o.pw, cands)
			}
			outs[i] = o
			var batch []extractOut
			if again {
				batch = []extractOut{o}
			} else if left--; left == 0 {
				batch = append([]extractOut(nil), outs...)
			}
			mu.Unlock()
			if batch != nil {
				a.postAssist(func() { a.extractSummary(batch) })
			}
			return o.err
		})
	}
}

// extractPasswordAsk is the task-card password prompt used after the book
// fails. Cancel / empty fails that archive; a typed password is retried.
func extractPasswordAsk(t *archiveTask) passwordPrompt {
	if t == nil || t.r == nil {
		return nil
	}
	r := t.r
	return func(name, detail string) (string, bool, error) {
		ans, err := r.Ask(tasks.Ask{
			Kind:    tasks.AskKindPassword,
			Item:    name,
			Detail:  detail,
			Options: []string{tasks.ChoiceSubmit, tasks.ChoiceCancel},
		})
		if err != nil {
			return "", false, err
		}
		if ans.Choice == tasks.ChoiceCancel {
			return "", false, errArchivePassword
		}
		return ans.Text, ans.Save, nil
	}
}

// extractOut is one archive's result.
type extractOut struct {
	src, name, dst, pw string
	inner              int
	locked             []string
	err                error
}

func extractOne(dir string, e Entry, candidates []string, ask passwordPrompt) extractOut {
	o := extractOut{src: e.Path, name: e.Name}
	defer aiFx.clearBusy(e.Path)
	base, err := uniqueFreeName(dir, archiveExtName(e.Name), true)
	if err != nil {
		o.err = err
		return o
	}
	dst := filepath.Join(dir, base)
	password, nested, err := extractArchiveNestedAsk(e.Path, dst, candidates, ask)
	if err != nil {
		_ = os.RemoveAll(dst)
		o.err = err
		return o
	}
	o.dst, o.pw = dst, password
	o.inner = len(nested.Extracted)
	o.locked = nested.Locked
	aiFx.markRenamed(e.Path, dst)
	return o
}

// extractSummary reloads, selects what came out and says how it went.
func (a *App) extractSummary(outs []extractOut) {
	var failed, extracted, locked, canceled, srcs []string
	var inner int
	for _, o := range outs {
		srcs = append(srcs, o.src)
		switch {
		case errors.Is(o.err, errArchiveCanceled):
			canceled = append(canceled, o.name)
		case o.err != nil:
			failed = append(failed, o.name)
		default:
			if o.pw != "" {
				a.lastArchivePassword = o.pw
			}
			inner += o.inner
			locked = append(locked, o.locked...)
			extracted = append(extracted, o.dst)
		}
	}
	{
		a.reload()
		if len(extracted) > 0 {
			a.cur().selected = map[string]bool{}
			for _, p := range extracted {
				a.cur().selected[p] = true
			}
			a.cur().anchor = extracted[0]
			a.requestPreview()
		}
		switch {
		case len(canceled) > 0 && len(failed) == 0:
			a.statusMsg = "已取消解压：" + strings.Join(canceled, "、")
		case len(failed) > 0:
			a.statusMsg = "解压失败：" + strings.Join(failed, "、")
		case len(locked) > 0:
			a.statusMsg = fmt.Sprintf("已解压 %d 个归档（%d 个内层）；%d 个内层归档密码本没解开，可在 设置 → 助手 添加密码后重试：%s",
				len(extracted), inner, len(locked), strings.Join(locked, "、"))
		case inner > 0:
			a.statusMsg = fmt.Sprintf("已解压 %d 个归档（自动连解 %d 个内层归档）", len(extracted), inner)
		case len(extracted) > 0:
			a.statusMsg = fmt.Sprintf("已解压 %d 个归档", len(extracted))
		}
		// Next step, offered with the toast: the bin holds the originals,
		// one click keeps only what they unpacked to.
		if len(failed) == 0 && len(extracted) > 0 {
			a.extractSources = srcs
		}
	}
}

// extractArchive 把 archive 解压到 dstDir（已存在且为空）。
func extractArchive(archive, dstDir string) error {
	switch archiveKind(filepath.Base(archive)) {
	case "zip":
		return extractZip(archive, dstDir)
	case "tar", "tgz", "tar.gz", "tbz2", "tar.bz2":
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		t := archiveTaskFor(archive)
		if st, err := f.Stat(); err == nil {
			t.reset(st.Size())
		}
		var r io.Reader = withTotalProgress(f, t)
		switch archiveKind(filepath.Base(archive)) {
		case "tgz", "tar.gz":
			if r, err = gzip.NewReader(r); err != nil {
				return err
			}
		case "tbz2", "tar.bz2":
			r = bzip2.NewReader(r)
		}
		return extractTarTask(r, dstDir, t)
	case "txz", "tar.xz":
		// bsdtar（macOS/Linux 自带，Windows 10+ 的 System32 里也有）原生读 xz。
		return runExtract(exec.Command("tar", "-xf", archive, "-C", dstDir))
	case "7z", "rar":
		// 7-Zip（Unix 的 7zz，Windows 的 7z.exe 或安装目录里的 7z）或
		// unar，谁装了用谁。
		if bin := sevenZipBin(); bin != "" {
			return runExtract(exec.Command(bin, "x", "-y", "-o"+dstDir, archive))
		}
		for _, c := range []*exec.Cmd{
			exec.Command("unar", "-force-overwrite", "-o", dstDir, archive),
			exec.Command("unrar", "x", "-y", archive, dstDir+string(filepath.Separator)),
		} {
			if path, err := exec.LookPath(c.Args[0]); err == nil {
				c.Path = path
				return runExtract(c)
			}
		}
		return errors.New("需要安装 7-Zip 或 unar 才能解压此格式")
	}
	return errors.New("不支持的归档格式")
}

// sevenZipBin finds a 7-Zip CLI: Homebrew's 7zz on Unix, a 7z on PATH, or
// the standard Windows install folder's 7z.exe.
func sevenZipBin() string {
	for _, name := range []string{"7zz", "7z"} {
		if p, err := archiveLookPath(name); err == nil {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		for _, dir := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, "7-Zip", "7z.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func runExtract(cmd *exec.Cmd) error {
	hideConsole(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// extractZip 解压 ZIP。跳过符号链接成员，拒绝越出 dstDir 的路径
// （zip-slip 防护）。
func extractZip(archive, dstDir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	t := archiveTaskFor(archive)
	if t != nil {
		var total int64
		files := 0
		for _, f := range zr.File {
			total += int64(f.UncompressedSize64)
			if !f.FileInfo().IsDir() {
				files++
			}
		}
		t.resetItems(total, files)
	}
	for _, f := range zr.File {
		if err := t.wait(); err != nil {
			return err
		}
		target, err := safeMember(dstDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		t.setItem(f.Name, int64(f.UncompressedSize64))
		if err := copyZipMember(f, target, t); err != nil {
			return err
		}
	}
	return nil
}

func copyZipMember(f *zip.File, target string, t *archiveTask) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, withProgress(rc, t))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(target, f.Modified, f.Modified)
}

// extractTar 解压 tar 流。目录与常规文件之外（符号链接、硬链接、设
// 备等）的成员一律跳过，越出 dstDir 的路径拒绝。
func extractTar(r io.Reader, dstDir string) error { return extractTarTask(r, dstDir, nil) }

// extractTarTask is extractTar naming each member on t; the bytes are
// counted on the archive file under r.
func extractTarTask(r io.Reader, dstDir string, t *archiveTask) error {
	tr := tar.NewReader(r)
	for {
		if err := t.wait(); err != nil {
			return err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Name == "" || h.Name == "." || h.Name == "/" {
			continue
		}
		target, err := safeMember(dstDir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			t.setItem(h.Name, h.Size)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, h.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, withItemProgress(tr, t))
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			continue
		}
		if !h.ModTime.IsZero() {
			_ = os.Chtimes(target, h.ModTime, h.ModTime)
		}
	}
}

// safeMember 把归档内的成员名映射到 dstDir 内的路径，拒绝绝对路径、
// 反斜杠穿越与“..”，防止 zip-slip。
func safeMember(dstDir, name string) (string, error) {
	name = filepath.ToSlash(name)
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("归档包含不安全的路径：" + name)
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("归档包含不安全的路径：" + name)
	}
	return filepath.Join(dstDir, clean), nil
}

// makeZip 把 entries 压缩为一个 ZIP 文件。文件直接以文件名入包；文件
// 夹以自身名字为前缀递归入包，与资源管理器压缩文件夹的行为一致。
func makeZip(dst string, entries []Entry) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	t := archiveTaskFor(dst)
	if t != nil && t.snap().Total <= 0 {
		t.reset(entriesSize(entries))
	}
	var addErr error
	for _, e := range entries {
		if addErr != nil {
			break
		}
		addErr = addTree(zw, e.Path, e.Name, e.IsDir, t)
	}
	if addErr == nil {
		addErr = zw.Close()
	}
	if closeErr := out.Close(); addErr == nil {
		addErr = closeErr
	}
	if addErr != nil {
		return addErr
	}
	return nil
}

// addTree 把 path（prefix 为归档内路径名）写入 zw，目录递归。
func addTree(zw *zip.Writer, path, prefix string, isDir bool, t *archiveTask) error {
	if !isDir {
		return addFile(zw, path, prefix, t)
	}
	hdr := &zip.FileHeader{Name: prefix + "/", Method: zip.Deflate}
	hdr.SetMode(0o755 | os.ModeDir)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_ = w
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		if d.IsDir() {
			h := &zip.FileHeader{Name: name + "/", Method: zip.Deflate}
			h.SetMode(0o755 | os.ModeDir)
			_, err := zw.CreateHeader(h)
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // 跳过符号链接
		}
		return addFile(zw, p, name, t)
	})
}

func addFile(zw *zip.Writer, path, name string, t *archiveTask) error {
	if err := t.wait(); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	t.setItem(name, info.Size())
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: info.ModTime()}
	hdr.SetMode(info.Mode().Perm())
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, withProgress(f, t))
	return err
}

// entriesSize is the bytes makeZip will read: regular files, folders
// walked, symlinks skipped.
func entriesSize(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		_ = filepath.WalkDir(e.Path, func(_ string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
				n += info.Size()
			}
			return nil
		})
	}
	return n
}

// uniqueFreeName 返回 dir 下不冲突的名字：优先原名，占用时按
// “名称 - 副本”“名称 - 副本 (2)”递增，与粘贴的命名风格一致。
func uniqueFreeName(dir, name string, isDir bool) (string, error) {
	if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
		return name, nil
	} else if err != nil {
		return "", err
	}
	candidate := fsops.CopyName(name, isDir, 1)
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fsops.CopyName(name, isDir, n)
	}
	return "", errors.New("无法生成不冲突的名称")
}
