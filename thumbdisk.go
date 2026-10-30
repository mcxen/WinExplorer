package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// thumbdisk.go 是缩略图与屏幕尺寸图的磁盘缓存：UserCacheDir/sunvale/thumbs。
// 用磁盘换时间——一张 24MP JPEG 解码要 0.8 s，读回它 256 px 的缩略图不到 1 ms。
// 键是 源路径 + 大小 + 修改时间 + 边长，文件一改键就变；旧条目按修改时间
// 最旧先删，总量守在 thumbDiskBudget 以内。不透明的图存 JPEG，带透明的存 PNG。

const (
	thumbDiskBudget = 1 << 30 // 1 GiB
	// thumbDiskTrimEvery 次写入后检查一次总量。
	thumbDiskTrimEvery = 128
)

var (
	// thumbDiskEnabled 在测试里关掉（thumbdisk_test.go），免得写进真实缓存目录。
	thumbDiskEnabled = true
	thumbDiskDirOnce sync.Once
	thumbDiskDirPath string
	thumbDiskWrites  atomic.Int64
	thumbDiskTrimMu  sync.Mutex

	// thumbDiskHits / thumbDiskMisses 供 spec 的命中率统计。
	thumbDiskHits, thumbDiskMisses atomic.Int64
)

// thumbDiskDir 是缓存目录；拿不到 UserCacheDir 时为空，磁盘缓存关闭。
func thumbDiskDir() string {
	thumbDiskDirOnce.Do(func() {
		if !thumbDiskEnabled {
			return
		}
		if d, err := os.UserCacheDir(); err == nil {
			thumbDiskDirPath = filepath.Join(d, stateDirName, "thumbs")
		}
	})
	return thumbDiskDirPath
}

func thumbDiskKey(path string, size int64, mtime time.Time, side int) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", path, size, mtime.UnixNano(), side)))
	k := hex.EncodeToString(h[:])
	return filepath.Join(k[:2], k)
}

// thumbDiskLoad 读回缓存的图；没有则 nil。
func thumbDiskLoad(path string, size int64, mtime time.Time, side int) image.Image {
	dir := thumbDiskDir()
	if dir == "" {
		return nil
	}
	f := filepath.Join(dir, thumbDiskKey(path, size, mtime, side))
	data, err := os.ReadFile(f)
	if err != nil {
		thumbDiskMisses.Add(1)
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		thumbDiskMisses.Add(1)
		return nil
	}
	thumbDiskHits.Add(1)
	now := time.Now()
	_ = os.Chtimes(f, now, now) // 最近用过的最后删
	return img
}

// thumbDiskEncode 是存盘的字节：不透明 JPEG q85，带透明 PNG。
func thumbDiskEncode(img image.Image) ([]byte, string) {
	var buf bytes.Buffer
	if o, ok := img.(interface{ Opaque() bool }); ok && !o.Opaque() {
		_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img)
		return buf.Bytes(), "image/png"
	}
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes(), "image/jpeg"
}

// thumbDiskStore 把 img 写进缓存（先写临时文件再改名，读者不会读到半张）。
func thumbDiskStore(path string, size int64, mtime time.Time, side int, img image.Image) {
	dir := thumbDiskDir()
	if dir == "" || img == nil {
		return
	}
	data, _ := thumbDiskEncode(img)
	thumbDiskStoreBytes(thumbDiskKey(path, size, mtime, side), data)
}

func thumbDiskStoreBytes(key string, data []byte) {
	dir := thumbDiskDir()
	if dir == "" || len(data) == 0 {
		return
	}
	f := filepath.Join(dir, key)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return
	}
	tmp := f + ".tmp" + fmt.Sprint(time.Now().UnixNano())
	if os.WriteFile(tmp, data, 0o644) != nil {
		os.Remove(tmp)
		return
	}
	if os.Rename(tmp, f) != nil {
		os.Remove(tmp)
		return
	}
	if thumbDiskWrites.Add(1)%thumbDiskTrimEvery == 0 {
		go thumbDiskTrim(dir, thumbDiskBudget)
	}
}

// thumbDiskTrim 删最旧的文件，直到总量回到 budget 的 80%。
func thumbDiskTrim(dir string, budget int64) {
	if !thumbDiskTrimMu.TryLock() {
		return
	}
	defer thumbDiskTrimMu.Unlock()
	type ent struct {
		p string
		n int64
		t time.Time
	}
	var all []ent
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			all = append(all, ent{p, info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	if total <= budget {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	for _, e := range all {
		if total <= budget*8/10 {
			break
		}
		if os.Remove(e.p) == nil {
			total -= e.n
		}
	}
}

// decodeFileCached 是 decodeFileScaled 前面加一层磁盘缓存：命中时读回小图，
// 未命中时解码、缩小、存盘。只用于比原图小的边长。
func decodeFileCached(path string, side int) (image.Image, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if img := thumbDiskLoad(path, info.Size(), info.ModTime(), side); img != nil {
		return img, nil
	}
	img, err := decodeFileScaled(path, side)
	if err != nil {
		return nil, err
	}
	// 原图本身就不比 side 大时没有必要存一份。
	if b := img.Bounds(); max(b.Dx(), b.Dy()) >= side || info.Size() > 512<<10 {
		thumbDiskStore(path, info.Size(), info.ModTime(), side, img)
	}
	return img, nil
}
