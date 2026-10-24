package driver

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"sunvale/internal/rename"
)

// local.go is the local driver: a folder on this machine, served with os.*.
// It is the default mount and the one the remote layer used before the
// driver abstraction existed — every operation here is the same call the
// handlers made directly, now behind the Reader/Writer interface so a
// mount can swap to SMB without the handler changing.

// Local is a local-folder mount. Root is the disk folder; paths inside the
// mount are joined onto it.
type Local struct {
	Root string
}

func (l *Local) Config() Config {
	return Config{ID: "local", Name: "本机路径", LocalSort: true}
}

// join maps a mount-relative path to its disk path, refusing any segment
// that would escape the root.
func (l *Local) join(p string) (string, bool) {
	clean := filepath.Clean("/" + p)
	for _, seg := range filepath.SplitList(clean) {
		if seg == ".." {
			return "", false
		}
	}
	rel := filepath.Clean(clean)
	if rel == "/" || rel == "." {
		return l.Root, true
	}
	return filepath.Join(l.Root, rel), true
}

func (l *Local) List(ctx context.Context, dir string) ([]Obj, error) {
	disk, ok := l.join(dir)
	if !ok {
		return nil, errPathEscape
	}
	entries, err := os.ReadDir(disk)
	if err != nil {
		return nil, err
	}
	out := make([]Obj, 0, len(entries))
	for _, de := range entries {
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, objFromInfo(de.Name(), disk, info))
	}
	return out, nil
}

func (l *Local) Get(ctx context.Context, p string) (Obj, error) {
	disk, ok := l.join(p)
	if !ok {
		return Obj{}, errPathEscape
	}
	info, err := os.Stat(disk)
	if err != nil {
		return Obj{}, err
	}
	return objFromInfo(filepath.Base(disk), filepath.Dir(disk), info), nil
}

func (l *Local) Open(ctx context.Context, p string) (io.ReadSeekCloser, error) {
	disk, ok := l.join(p)
	if !ok {
		return nil, errPathEscape
	}
	return os.Open(disk)
}

func (l *Local) Mkdir(ctx context.Context, p string) error {
	disk, ok := l.join(p)
	if !ok {
		return errPathEscape
	}
	return os.Mkdir(disk, 0o755)
}

func (l *Local) Rename(ctx context.Context, p, newName string) error {
	disk, ok := l.join(p)
	if !ok {
		return errPathEscape
	}
	return os.Rename(disk, filepath.Join(filepath.Dir(disk), newName))
}

func (l *Local) Move(ctx context.Context, src, dst string) error {
	s, ok := l.join(src)
	if !ok {
		return errPathEscape
	}
	d, ok := l.join(dst)
	if !ok {
		return errPathEscape
	}
	return moveEntry(s, d)
}

func (l *Local) Copy(ctx context.Context, src, dst string) error {
	s, ok := l.join(src)
	if !ok {
		return errPathEscape
	}
	d, ok := l.join(dst)
	if !ok {
		return errPathEscape
	}
	info, err := os.Lstat(s)
	if err != nil {
		return err
	}
	return copyEntry(s, d, info)
}

func (l *Local) Remove(ctx context.Context, p string) error {
	disk, ok := l.join(p)
	if !ok {
		return errPathEscape
	}
	return os.RemoveAll(disk)
}

func (l *Local) Put(ctx context.Context, dst string, r io.Reader) error {
	disk, ok := l.join(dst)
	if !ok {
		return errPathEscape
	}
	if err := os.MkdirAll(filepath.Dir(disk), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(disk), ".sunvale-upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpName)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	return os.Rename(tmpName, disk)
}

func (l *Local) Drop(ctx context.Context) error { return nil }

// objFromInfo builds an Obj from an os.FileInfo, filling Created from the
// platform birth time when it is available.
func objFromInfo(name, dir string, info os.FileInfo) Obj {
	created := info.ModTime()
	if bt, err := rename.BirthTime(filepath.Join(dir, name)); err == nil {
		created = bt
	}
	size := info.Size()
	if info.IsDir() {
		size = 0
	}
	return Obj{Name: name, Size: size, IsDir: info.IsDir(), ModTime: info.ModTime(), Created: created}
}

// moveEntry and copyEntry are the os.* implementations the remote layer
// already had (remote_manage.go); they live here now so both drivers share
// them and the handlers do not reach into os directly.
func moveEntry(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := copyEntry(src, dst, info); err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(src)
	}
	return os.Remove(src)
}

func copyEntry(src, dst string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			s := filepath.Join(src, e.Name())
			fi, err := os.Lstat(s)
			if err != nil {
				return err
			}
			if err := copyEntry(s, filepath.Join(dst, e.Name()), fi); err != nil {
				return err
			}
		}
		return os.Chmod(dst, info.Mode())
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// errPathEscape is returned when a mount-relative path would leave the
// mount root. The handlers map it to the protocol's invalid-path answer.
var errPathEscape = pathEscape("path escapes the mount")

type pathEscape string

func (e pathEscape) Error() string { return string(e) }
