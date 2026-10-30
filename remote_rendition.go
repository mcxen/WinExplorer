package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// remote_rendition.go 是 /d/…?w=N：把图片缩到最长边 N px 再发，手机看图和网格
// 缩略图就不用下原图（一张 24MP JPEG 8 MB，2048 px 的约 0.6 MB）。结果存在
// 磁盘缓存（thumbdisk.go 同一目录）里，键是 虚拟路径 + 大小 + 修改时间 + N；
// ETag 也由它们组成，Cache-Control 让浏览器一周内直接用本地副本——文件一改
// 修改时间就变，URL 里的签名和 ETag 也随之变化。不带 w 的请求照旧发原图。

// renditionSides 是允许的边长：取整到这几档，缓存才能命中。
var renditionSides = []int{256, 512, 1024, 1600, 2048, 3072}

// renditionSide 把 ?w= 取整到最近的上一档；没有或不合法时 false。
func renditionSide(q string) (int, bool) {
	if q == "" {
		return 0, false
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return 0, false
	}
	for _, s := range renditionSides {
		if n <= s {
			return s, true
		}
	}
	return 0, false // 比最大档还大：发原图
}

// renditionSem 限制同时解码的张数：网格一次要几十张缩略图，全并发解大图会
// 把内存撑到几个 GB。
var renditionSem = make(chan struct{}, 3)

// renditionMaxSource 以上的原图不在服务端解码（太大的图解一次要上 GB 内存）。
const renditionMaxSource = 64 << 20

// serveRendition 发 side px 的版本；false 表示做不出来（调用方改发原图）。
func serveRendition(w http.ResponseWriter, r *http.Request, vpath, name string, size int64, mtime time.Time, side int, src io.Reader) bool {
	if size > renditionMaxSource {
		return false
	}
	etag := fmt.Sprintf(`"%x-%x-w%d"`, mtime.Unix(), size, side)
	key := thumbDiskKey("remote:"+vpath, size, mtime, side) + ".r"
	data, ctype := renditionLoad(key)
	if data == nil {
		renditionSem <- struct{}{}
		defer func() { <-renditionSem }()
		raw, err := io.ReadAll(src)
		if err != nil {
			return false
		}
		img, err := decodeBytesScaled(raw, side)
		if err != nil {
			return false
		}
		data, ctype = thumbDiskEncode(img)
		if len(data) == 0 {
			return false
		}
		thumbDiskStoreBytes(key, data)
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Etag", etag)
	h.Set("Cache-Control", "private, max-age=604800")
	h.Set("Content-Disposition", fmt.Sprintf(`inline; filename*=utf-8''%s`, url.PathEscape(name)))
	http.ServeContent(w, r, "", mtime, bytes.NewReader(data))
	return true
}

func renditionLoad(key string) ([]byte, string) {
	dir := thumbDiskDir()
	if dir == "" {
		return nil, ""
	}
	data, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil || len(data) < 4 {
		return nil, ""
	}
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		return data, "image/png"
	}
	return data, "image/jpeg"
}
