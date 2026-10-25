package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// pathWithin reports whether path is dir or a descendant of dir.
func PathWithin(path, dir string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func PasteOne(src, dstDir string) (string, error) {
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}
	name, err := UniqueCopyName(dstDir, filepath.Base(src), info.IsDir())
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, name)
	if info.IsDir() {
		if err := CopyDir(src, dst); err != nil {
			return "", err
		}
		return name, nil
	}
	if err := CopyFile(src, dst, info); err != nil {
		return "", err
	}
	return name, nil
}

// uniqueCopyName is Windows-style: "名称 - 副本", then "名称 - 副本 (2)",
// with the suffix before the extension for files.
func UniqueCopyName(dir, name string, isDir bool) (string, error) {
	candidate := CopyName(name, isDir, 1)
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = CopyName(name, isDir, n)
	}
	return "", fmt.Errorf("无法生成不冲突的名称")
}

func CopyName(name string, isDir bool, n int) string {
	suffix := " - 副本"
	if n > 1 {
		suffix = fmt.Sprintf(" - 副本 (%d)", n)
	}
	if isDir {
		return name + suffix
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		return name + suffix
	}
	return stem + suffix + ext
}

func CopyFile(src, dst string, info os.FileInfo) error { return CopyFileH(src, dst, info, nil) }

// Hooks let a caller watch and steer a copy: Item names each file (its
// path relative to the copy's root) before its bytes, Wrap wraps its
// reader (progress, pause, cancel), Wait runs between files. Any may be
// nil; a nil *Hooks copies plainly.
type Hooks struct {
	Item func(rel string, size int64)
	Done func()
	Wrap func(io.Reader) io.Reader
	Wait func() error
	root string
}

func (h *Hooks) item(path string, size int64) error {
	if h == nil {
		return nil
	}
	if h.Wait != nil {
		if err := h.Wait(); err != nil {
			return err
		}
	}
	if h.Item != nil {
		rel := filepath.Base(path)
		if h.root != "" {
			if r, err := filepath.Rel(h.root, path); err == nil {
				rel = filepath.ToSlash(filepath.Join(filepath.Base(h.root), r))
			}
		}
		h.Item(rel, size)
	}
	return nil
}

func (h *Hooks) done() {
	if h != nil && h.Done != nil {
		h.Done()
	}
}

func (h *Hooks) wrap(r io.Reader) io.Reader {
	if h == nil || h.Wrap == nil {
		return r
	}
	return h.Wrap(r)
}

// CopyFileH is CopyFile under h.
func CopyFileH(src, dst string, info os.FileInfo, h *Hooks) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if err := h.item(src, info.Size()); err != nil {
		return err
	}
	defer h.done()
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, h.wrap(in))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

func CopyDir(src, dst string) error { return CopyDirH(src, dst, nil) }

// CopyDirH is CopyDir under h; Item names files relative to src's parent.
func CopyDirH(src, dst string, h *Hooks) error {
	srcClean := filepath.Clean(src)
	seen := map[string]struct{}{srcClean: {}}
	if h != nil {
		hh := *h
		hh.root = srcClean
		h = &hh
	}
	return copyDirRec(srcClean, dst, seen, h)
}

func copyDirRec(src, dst string, seen map[string]struct{}, h *Hooks) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		fi, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := CopyFileH(srcPath, dstPath, fi, h); err != nil {
				return err
			}
			continue
		}
		if fi.IsDir() {
			real, err := filepath.EvalSymlinks(srcPath)
			if err == nil {
				real = filepath.Clean(real)
				if _, ok := seen[real]; ok {
					continue
				}
				seen[real] = struct{}{}
			}
			if err := copyDirRec(srcPath, dstPath, seen, h); err != nil {
				return err
			}
			continue
		}
		if err := CopyFileH(srcPath, dstPath, fi, h); err != nil {
			return err
		}
	}
	return nil
}

// TreeSize is the bytes and regular files under path (path itself when
// it is a file); symlinks count as zero-byte items.
func TreeSize(path string) (bytes int64, files int) {
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	return
}

func UniqueTrashName(dir, name string) (string, error) {
	candidate := name
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem = name
		ext = ""
	}
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s %d%s", stem, n, ext)
	}
	return "", fmt.Errorf("废纸篓中名称冲突")
}

func UniqueNewFolderName(dir string) (string, error) {
	candidate := "新建文件夹"
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("新建文件夹 (%d)", n)
	}
	return "", fmt.Errorf("无法生成不冲突的名称")
}
