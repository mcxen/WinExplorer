package main

import (
	"encoding/base64"
	"os"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// forms_media.go loads local paths and data:image URLs for OpenUI
// KindImage / KindVideo / KindList thumbs. Failures stay cached so a
// broken path does not retry every frame.

type formMediaEntry struct {
	bm  *ui.Bitmap
	err string
}

var formMediaCache sync.Map // key string -> *formMediaEntry

const formMediaSide = 512
const formThumbSide = 64

func formMediaKey(src string, side int) string {
	return src + "\x00" + itoa(side)
}

// formLoadBitmap decodes src (absolute path or data:image/...;base64,...) into
// a Bitmap scaled to side. Returns (nil, reason) on failure.
func formLoadBitmap(src string, side int) (*ui.Bitmap, string) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, "无图片"
	}
	if side <= 0 {
		side = formMediaSide
	}
	key := formMediaKey(src, side)
	if v, ok := formMediaCache.Load(key); ok {
		e := v.(*formMediaEntry)
		return e.bm, e.err
	}
	bm, errMsg := formDecodeBitmap(src, side)
	formMediaCache.Store(key, &formMediaEntry{bm: bm, err: errMsg})
	return bm, errMsg
}

func formDecodeBitmap(src string, side int) (*ui.Bitmap, string) {
	data, errMsg := formReadBytes(src)
	if data == nil {
		return nil, errMsg
	}
	img, err := decodeBytesScaled(data, side)
	if err != nil {
		// Video paths: try a poster frame when the bytes are not an image.
		if isVideoExt(lowerExt(src)) {
			if poster, e := videoPosterImage(src, side); e == nil && poster != nil {
				return ui.NewBitmap(poster), ""
			}
		}
		return nil, "无法解码图片"
	}
	return ui.NewBitmap(img), ""
}

func formReadBytes(src string) ([]byte, string) {
	if strings.HasPrefix(src, "data:image/") {
		return formDecodeDataURL(src)
	}
	// Reject obvious non-files early.
	if strings.Contains(src, "://") {
		return nil, "不支持的图片地址"
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, "无法读取文件"
	}
	if len(data) == 0 {
		return nil, "文件为空"
	}
	return data, ""
}

func formDecodeDataURL(src string) ([]byte, string) {
	i := strings.Index(src, ",")
	if i < 0 {
		return nil, "无效 data URL"
	}
	meta, payload := src[:i], src[i+1:]
	if !strings.Contains(meta, ";base64") {
		return nil, "仅支持 base64 data URL"
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Some emitters omit padding.
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return nil, "data URL 解码失败"
	}
	return data, ""
}

// formLoadPoster returns a video keyframe bitmap, or a soft error string.
func formLoadPoster(path string, side int) (*ui.Bitmap, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "无视频"
	}
	if side <= 0 {
		side = formMediaSide
	}
	key := formMediaKey("poster:"+path, side)
	if v, ok := formMediaCache.Load(key); ok {
		e := v.(*formMediaEntry)
		return e.bm, e.err
	}
	img, err := videoPosterImage(path, side)
	if err != nil || img == nil {
		msg := "无法提取关键帧"
		if err != nil {
			msg = "无法提取关键帧（需 ffmpeg）"
		}
		formMediaCache.Store(key, &formMediaEntry{err: msg})
		return nil, msg
	}
	bm := ui.NewBitmap(img)
	formMediaCache.Store(key, &formMediaEntry{bm: bm})
	return bm, ""
}
