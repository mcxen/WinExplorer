package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AbsFrom resolves p against base without consulting the process working
// directory. An empty base keeps the old absolute-path-only contract.
func AbsFrom(base, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		p = base
	}
	if p == "" {
		return "", fmt.Errorf("需要绝对路径")
	}
	if !filepath.IsAbs(p) {
		if base == "" {
			return "", fmt.Errorf("需要绝对路径")
		}
		if !filepath.IsAbs(base) {
			return "", fmt.Errorf("基准路径必须是绝对路径")
		}
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p), nil
}

// FreeName returns name when it is unused, otherwise Windows' copy naming.
func FreeName(dir, name string, isDir bool) (string, error) {
	if _, err := os.Lstat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name, nil
	} else if err != nil {
		return "", err
	}
	return UniqueCopyName(dir, name, isDir)
}

// PlaceItem copies one item to an exact destination. A move first tries the
// cheap rename; when volumes force a copy, the source is removed only after
// the complete destination is in place.
func PlaceItem(src, dst string, move bool) error {
	if move {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := CopyDir(src, dst); err != nil {
			return err
		}
	} else if err := CopyFile(src, dst, info); err != nil {
		return err
	}
	if move {
		return os.RemoveAll(src)
	}
	return nil
}
