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
	"strings"
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

// isArchive 报告 name 是否为受支持的归档。
func isArchive(name string) bool { return archiveKind(name) != "" }

// doCompress 把当前选择压缩为当前文件夹中的一个 ZIP。单个项目以它
// 命名，多个项目以“压缩文件”命名，重名时按 Windows 风格加“ - 副本”。
func (a *App) doCompress() {
	sel := a.selectedEntries()
	if len(sel) == 0 || a.cur().path == pathThisPC {
		return
	}
	stem := "压缩文件"
	if len(sel) == 1 {
		stem = strings.TrimSuffix(sel[0].Name, filepath.Ext(sel[0].Name))
	}
	name, err := uniqueFreeName(a.cur().path, stem+".zip", false)
	if err != nil {
		a.statusMsg = "压缩失败：" + err.Error()
		return
	}
	dst := filepath.Join(a.cur().path, name)
	if err := makeZip(dst, sel); err != nil {
		_ = os.Remove(dst)
		a.statusMsg = "压缩失败：" + err.Error()
		return
	}
	a.reload()
	a.selectOnly(dst)
	a.statusMsg = "已创建 " + name
}

// doExtract 解压选中的每个归档到以其基础名命名的子文件夹。
func (a *App) doExtract() {
	sel := a.selectedEntries()
	if len(sel) == 0 || a.cur().path == pathThisPC {
		return
	}
	var failed []string
	var extracted []string
	for _, e := range sel {
		if e.IsDir || !isArchive(e.Name) {
			continue
		}
		base, err := uniqueFreeName(a.cur().path, archiveExtName(e.Name), true)
		if err != nil {
			failed = append(failed, e.Name)
			continue
		}
		dst := filepath.Join(a.cur().path, base)
		if err := extractArchive(e.Path, dst); err != nil {
			_ = os.RemoveAll(dst)
			failed = append(failed, e.Name)
			continue
		}
		extracted = append(extracted, dst)
	}
	a.reload()
	if len(extracted) > 0 {
		a.cur().selected = map[string]bool{}
		for _, p := range extracted {
			a.cur().selected[p] = true
		}
		a.cur().anchor = extracted[0]
		a.requestPreview()
	}
	if len(failed) > 0 {
		a.statusMsg = "解压失败：" + strings.Join(failed, "、")
	} else if len(extracted) > 0 {
		a.statusMsg = fmt.Sprintf("已解压 %d 个归档", len(extracted))
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
		var r io.Reader = f
		switch archiveKind(filepath.Base(archive)) {
		case "tgz", "tar.gz":
			if r, err = gzip.NewReader(f); err != nil {
				return err
			}
		case "tbz2", "tar.bz2":
			r = bzip2.NewReader(f)
		}
		return extractTar(r, dstDir)
	case "txz", "tar.xz":
		// bsdtar（macOS/Linux 自带）原生读 xz。
		return runExtract(exec.Command("tar", "-xf", archive, "-C", dstDir))
	case "7z", "rar":
		// 7-Zip 或 unar，谁装了用谁。
		for _, c := range []*exec.Cmd{
			exec.Command("7zz", "x", "-y", "-o"+dstDir, archive),
			exec.Command("unar", "-force-overwrite", "-o", dstDir, archive),
			exec.Command("unrar", "x", "-y", archive, dstDir+string(filepath.Separator)),
		} {
			if path, err := exec.LookPath(c.Args[0]); err == nil {
				c.Path = path
				return runExtract(c)
			}
		}
		return errors.New("需要安装 7-Zip（7zz）或 unar 才能解压此格式")
	}
	return errors.New("不支持的归档格式")
}

func runExtract(cmd *exec.Cmd) error {
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
	for _, f := range zr.File {
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
		if err := copyZipMember(f, target); err != nil {
			return err
		}
	}
	return nil
}

func copyZipMember(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, rc)
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
func extractTar(r io.Reader, dstDir string) error {
	tr := tar.NewReader(r)
	for {
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
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, h.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
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
	var addErr error
	for _, e := range entries {
		if addErr != nil {
			break
		}
		addErr = addTree(zw, e.Path, e.Name, e.IsDir)
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
func addTree(zw *zip.Writer, path, prefix string, isDir bool) error {
	if !isDir {
		return addFile(zw, path, prefix)
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
		return addFile(zw, p, name)
	})
}

func addFile(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: info.ModTime()}
	hdr.SetMode(info.Mode().Perm())
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// uniqueFreeName 返回 dir 下不冲突的名字：优先原名，占用时按
// “名称 - 副本”“名称 - 副本 (2)”递增，与粘贴的命名风格一致。
func uniqueFreeName(dir, name string, isDir bool) (string, error) {
	if _, err := os.Lstat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
		return name, nil
	} else if err != nil {
		return "", err
	}
	candidate := copyName(name, isDir, 1)
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = copyName(name, isDir, n)
	}
	return "", errors.New("无法生成不冲突的名称")
}
