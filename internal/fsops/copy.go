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

func CopyFile(src, dst string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
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

func CopyDir(src, dst string) error {
	srcClean := filepath.Clean(src)
	seen := map[string]struct{}{srcClean: {}}
	return copyDirRec(srcClean, dst, seen)
}

func copyDirRec(src, dst string, seen map[string]struct{}) error {
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
			if err := CopyFile(srcPath, dstPath, fi); err != nil {
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
			if err := copyDirRec(srcPath, dstPath, seen); err != nil {
				return err
			}
			continue
		}
		if err := CopyFile(srcPath, dstPath, fi); err != nil {
			return err
		}
	}
	return nil
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
