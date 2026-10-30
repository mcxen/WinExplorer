package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// volsave.go is 图片快存: the image being previewed is copied into one
// target folder (↑ / volume-up), the last copy can be taken back (↓ /
// volume-down), and the same source saved again within volSaveDedupe only
// answers 已保存过. The desktop quick look and the remote web page share
// this state, and the target folder lives in volsave.json beside the other
// config files.

const volSaveDedupe = time.Minute

var (
	errVolSaveNoTarget = errors.New("未设置快存目录")
	errVolSaveNothing  = errors.New("没有可取消的保存")
	errVolSaveChanged  = errors.New("副本已变更，未删除")
	errVolSaveNotImage = errors.New("不是图片")
)

type volSaveRec struct {
	src, dst string
	at       time.Time
	size     int64
	mod      time.Time
}

var (
	volSaveMu     sync.Mutex
	volSaveRecent = map[string]*volSaveRec{}
	volSaveLast   *volSaveRec
	volSaveNow    = time.Now
	// volSaveConfFile overrides the config path in tests.
	volSaveConfFile string
)

func volSaveConfPath() string {
	if volSaveConfFile != "" {
		return volSaveConfFile
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "volsave.json")
}

// volSaveTarget is the configured target folder, "" when unset.
func volSaveTarget() string {
	p := volSaveConfPath()
	if p == "" {
		return ""
	}
	var c struct {
		Dir string `json:"dir"`
	}
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c.Dir
}

func setVolSaveTarget(dir string) error {
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return errors.New("目录不存在")
		}
		dir = filepath.Clean(dir)
	}
	p := volSaveConfPath()
	if p == "" {
		return errors.New("无配置目录")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"dir": dir})
	return os.WriteFile(p, data, 0o600)
}

var volSaveImageExt = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "bmp": true,
	"heic": true, "heif": true, "tif": true, "tiff": true, "avif": true, "svg": true,
}

func volSaveIsImage(path string) bool {
	return volSaveImageExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

// volSaveCopy copies src into dir under a free name (name (1).ext …).
// dup is true, and nothing is copied, when the same src was saved less
// than volSaveDedupe ago.
func volSaveCopy(src, dir string) (dst string, dup bool, err error) {
	if dir == "" {
		return "", false, errVolSaveNoTarget
	}
	src = filepath.Clean(src)
	if !volSaveIsImage(src) {
		return "", false, errVolSaveNotImage
	}
	info, err := os.Stat(src)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, errors.New("源文件不存在")
	}
	if d, err := os.Stat(dir); err != nil || !d.IsDir() {
		return "", false, errors.New("快存目录不存在")
	}
	volSaveMu.Lock()
	defer volSaveMu.Unlock()
	now := volSaveNow()
	if r := volSaveRecent[src]; r != nil && now.Sub(r.at) < volSaveDedupe {
		return r.dst, true, nil
	}
	in, err := os.Open(src)
	if err != nil {
		return "", false, err
	}
	defer in.Close()
	ext := filepath.Ext(src)
	base := strings.TrimSuffix(filepath.Base(src), ext)
	var out *os.File
	for i := 0; i < 10000; i++ {
		name := base + ext
		if i > 0 {
			name = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		dst = filepath.Join(dir, name)
		out, err = os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil || !errors.Is(err, os.ErrExist) {
			break
		}
	}
	if err != nil {
		return "", false, err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", false, err
	}
	if err = out.Close(); err != nil {
		os.Remove(dst)
		return "", false, err
	}
	st, err := os.Stat(dst)
	if err != nil {
		return "", false, err
	}
	r := &volSaveRec{src: src, dst: dst, at: now, size: st.Size(), mod: st.ModTime()}
	volSaveRecent[src] = r
	volSaveLast = r
	for k, v := range volSaveRecent {
		if now.Sub(v.at) >= volSaveDedupe {
			delete(volSaveRecent, k)
		}
	}
	return dst, false, nil
}

// volSaveUndo deletes the copy the last save made, only while it is the
// same file that save wrote: same size and modification time.
func volSaveUndo() (string, error) {
	volSaveMu.Lock()
	defer volSaveMu.Unlock()
	r := volSaveLast
	if r == nil {
		return "", errVolSaveNothing
	}
	st, err := os.Lstat(r.dst)
	if err != nil || !st.Mode().IsRegular() || st.Size() != r.size || !st.ModTime().Equal(r.mod) {
		volSaveLast = nil
		return "", errVolSaveChanged
	}
	if err := os.Remove(r.dst); err != nil {
		return "", err
	}
	volSaveLast = nil
	if volSaveRecent[r.src] == r {
		delete(volSaveRecent, r.src)
	}
	return r.dst, nil
}

// volSaveMsg is the toast for a save outcome.
func volSaveMsg(dup bool, err error) string {
	switch {
	case err != nil:
		return "快存失败：" + err.Error()
	case dup:
		return "已保存过"
	}
	return "已保存"
}

// volSaveToast shows msg as a toast even when it repeats the last one, so a
// second 已保存过 still answers the key.
func (a *App) volSaveToast(msg string) {
	a.statusToasted = ""
	a.statusMsg = msg
}

// volSaveTargetLabel describes the target for the settings row.
func volSaveTargetLabel() string {
	if d := volSaveTarget(); d != "" {
		return d + " · 快速预览中 ↑ 保存 ↓ 取消"
	}
	return "未设置 · 快速预览中 ↑ 保存 ↓ 取消"
}
