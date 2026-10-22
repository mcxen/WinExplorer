package fileview

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

// media.go is the picture side of the extractor: the image itself, frames
// pulled out of a video, and the stream metadata that puts numbers on what
// the model is looking at. Videos get real frames rather than one poster
// because "what happens in this clip" is answered by several moments, not
// by the first one.

// PosterFunc renders a still of any file the OS can render: Quick Look on
// macOS covers HEIC, PDF pages, Pages documents and more. The application
// installs it (platform code); tests leave it nil so nothing here depends
// on a particular machine.
var PosterFunc func(path string, side int) (image.Image, error)

// videoFrameTimes picks the moments a clip is sampled at: never the very
// first frame (title cards, fade-ins) and spread over the runtime.
func videoFrameTimes(duration float64, count int) []float64 {
	if count <= 0 {
		return nil
	}
	if duration <= 0 {
		return []float64{1, 3, 6, 10}
	}
	if duration < 2 {
		return []float64{duration / 2}
	}
	out := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		// 10% in and 10% out keep the sample away from black frames and
		// end credits.
		out = append(out, duration*(0.1+0.8*float64(i)/float64(max(count-1, 1))))
	}
	return out
}

func extractImage(path string, info os.FileInfo, opt Options, res Result) Result {
	res.Method = "内置解析"
	img, err := decodeImage(path)
	if err == nil {
		b := img.Bounds()
		res.Meta.Width, res.Meta.Height = b.Dx(), b.Dy()
		if frame, ok := encodeFrame("原图", img, opt.ImageSide); ok {
			res.Frames = append(res.Frames, frame)
		}
		res.Text = fmt.Sprintf("图片 %s，%d × %d 像素，%s。画面见附图。",
			filepath.Base(path), b.Dx(), b.Dy(), formatSize(info.Size()))
		if extOf(path) == "gif" {
			res.Text += "这是动图，附图是第一帧。"
		}
		return res
	}
	// Formats Go cannot decode (HEIC, AVIF, TIFF, camera RAW): let the OS
	// or the script render them instead of giving up.
	res.Warnings = append(res.Warnings, "内置解码失败："+err.Error())
	if out, ok := runScript(path, opt, res.Kind); ok {
		res = mergeScript(res, out, opt)
		if len(res.Frames) > 0 {
			return res
		}
	}
	if PosterFunc != nil {
		if img, err := PosterFunc(path, opt.ImageSide*2); err == nil && img != nil {
			b := img.Bounds()
			res.Meta.Width, res.Meta.Height = b.Dx(), b.Dy()
			if frame, ok := encodeFrame("原图", img, opt.ImageSide); ok {
				res.Frames = append(res.Frames, frame)
				res.Method = "系统渲染"
				res.Text = fmt.Sprintf("图片 %s，%d × %d 像素，%s。画面见附图。",
					filepath.Base(path), b.Dx(), b.Dy(), formatSize(info.Size()))
				return res
			}
		}
	}
	res.Text = fmt.Sprintf("这是图片 %s（%s），这台机器上没能解出画面。%s",
		filepath.Base(path), formatSize(info.Size()), strings.Join(res.Warnings, "；"))
	return res
}

func extractVideo(path string, info os.FileInfo, opt Options, res Result) Result {
	if !opt.NoExternal {
		if meta, ok := ffprobeMeta(path); ok {
			res.Meta = meta
		}
	}
	times := videoFrameTimes(res.Meta.Duration, opt.MaxImages)
	if len(times) == 0 {
		times = videoFrameTimes(0, min(opt.MaxImages, 3))
	}
	for _, t := range times {
		if len(res.Frames) >= opt.MaxImages {
			break
		}
		if opt.NoExternal {
			break
		}
		img, ok := ffmpegFrame(path, t, opt.ImageSide)
		if !ok {
			continue
		}
		label := fmt.Sprintf("%s 画面", formatClock(t))
		if frame, ok := encodeFrame(label, img, opt.ImageSide); ok {
			res.Frames = append(res.Frames, frame)
		}
	}
	if len(res.Frames) > 0 {
		res.Method = "ffmpeg 抽帧"
	} else {
		// No ffmpeg: the script (OpenCV) or the OS poster is the fallback.
		if out, ok := runScript(path, opt, res.Kind); ok {
			res = mergeScript(res, out, opt)
			if len(res.Frames) > 0 {
				res.Method = "Python 脚本"
			}
		}
		if len(res.Frames) == 0 && PosterFunc != nil {
			if img, err := PosterFunc(path, opt.ImageSide*2); err == nil && img != nil {
				if frame, ok := encodeFrame("首帧", img, opt.ImageSide); ok {
					res.Frames = append(res.Frames, frame)
					res.Method = "系统渲染"
				}
			}
		}
		if len(res.Frames) == 0 {
			res.Warnings = append(res.Warnings, "没有可用的抽帧方式（需要 ffmpeg 或带 OpenCV 的 Python）")
		}
	}
	res.Text = videoSummary(path, info, res)
	return res
}

func videoSummary(path string, info os.FileInfo, res Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "视频 %s，%s", filepath.Base(path), formatSize(info.Size()))
	if res.Meta.Duration > 0 {
		fmt.Fprintf(&b, "，时长 %s", formatClock(res.Meta.Duration))
	}
	if res.Meta.Width > 0 {
		fmt.Fprintf(&b, "，%d × %d", res.Meta.Width, res.Meta.Height)
	}
	if res.Meta.VideoCodec != "" {
		fmt.Fprintf(&b, "，视频编码 %s", res.Meta.VideoCodec)
	}
	if res.Meta.AudioCodec != "" {
		fmt.Fprintf(&b, "，音频编码 %s", res.Meta.AudioCodec)
	}
	b.WriteString("。")
	if len(res.Frames) > 0 {
		fmt.Fprintf(&b, "按时间顺序附上 %d 张画面帧，看图了解内容。", len(res.Frames))
	} else {
		b.WriteString("这台机器上没能抽出画面帧。")
	}
	if len(res.Warnings) > 0 {
		b.WriteString(strings.Join(res.Warnings, "；"))
	}
	return b.String()
}

func extractAudio(path string, info os.FileInfo, opt Options, res Result) Result {
	res.Method = "ffprobe"
	var ok bool
	if !opt.NoExternal {
		if meta, found := ffprobeMeta(path); found {
			res.Meta, ok = meta, true
		}
	}
	if !ok {
		if out, ok := runScript(path, opt, res.Kind); ok {
			res = mergeScript(res, out, opt)
			res.Method = "Python 脚本"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "音频 %s，%s", filepath.Base(path), formatSize(info.Size()))
	if res.Meta.Duration > 0 {
		fmt.Fprintf(&b, "，时长 %s", formatClock(res.Meta.Duration))
	}
	if res.Meta.AudioCodec != "" {
		fmt.Fprintf(&b, "，编码 %s", res.Meta.AudioCodec)
	}
	b.WriteString("。")
	if len(res.Frames) > 0 {
		b.WriteString("附上声谱图，可以看图判断节奏与静音段。")
	} else {
		b.WriteString("音频内容无法直接转成文字，需要转写（语音识别）才能读出说话内容。")
	}
	res.Text = b.String()
	return res
}

// decodeImage decodes a picture with whatever format support the build has
// registered. Callers treat a failure as "try another route", not as a dead
// end, because HEIC and friends land here.
func decodeImage(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 96<<20 {
		return nil, fmt.Errorf("图片太大（%s），超过 96 MB", formatSize(int64(len(data))))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// encodeFrame scales and re-encodes one picture for the model. JPEG keeps
// the payload small enough that four frames are still cheap in tokens;
// anything with alpha becomes PNG so transparency is not lost.
func encodeFrame(label string, img image.Image, side int) (Frame, bool) {
	if img == nil {
		return Frame{}, false
	}
	img = scaleToFit(img, side)
	b := img.Bounds()
	var (
		buf  bytes.Buffer
		mime = "image/jpeg"
		err  error
	)
	if hasAlpha(img) {
		mime = "image/png"
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82})
	}
	if err != nil {
		return Frame{}, false
	}
	if buf.Len() == 0 {
		return Frame{}, false
	}
	return Frame{Label: label, MIME: mime, Data: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, true
}

// hasAlpha reports whether the picture would show background through. The
// bounds check matters: a decoder may return a non-RGBA image whose opaque
// pixels still carry alpha of zero on some exotic formats.
func hasAlpha(img image.Image) bool {
	type rgba interface {
		At(x, y int) (r, g, b, a uint32)
	}
	b := img.Bounds()
	// Sample a small grid rather than every pixel: a 4000×3000 photo would
	// otherwise spend all its time proving it is opaque.
	stepX := max(1, b.Dx()/16)
	stepY := max(1, b.Dy()/16)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0xffff {
				return true
			}
		}
	}
	return false
}

func scaleToFit(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || max(w, h) <= side {
		return img
	}
	scale := float64(side) / float64(max(w, h))
	tw := max(1, int(float64(w)*scale+0.5))
	th := max(1, int(float64(h)*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}

// ffprobeMeta reads the streams of a media file. The command is the same one
// the preview pane uses, so the assistant and the pane quote the same
// duration for the same clip.
func ffprobeMeta(path string) (Meta, bool) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return Meta{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return Meta{}, false
	}
	var payload struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Meta{}, false
	}
	var meta Meta
	if payload.Format.Duration != "" {
		if d, err := strconv.ParseFloat(payload.Format.Duration, 64); err == nil {
			meta.Duration = d
		}
	}
	for _, s := range payload.Streams {
		switch s.CodecType {
		case "video":
			if meta.Width == 0 {
				meta.Width, meta.Height = s.Width, s.Height
				meta.VideoCodec = strings.ToUpper(s.CodecName)
			}
		case "audio":
			if meta.AudioCodec == "" {
				meta.AudioCodec = strings.ToUpper(s.CodecName)
			}
		}
	}
	return meta, true
}

// ffmpegFrame renders one moment of a clip to a decoded picture.
func ffmpegFrame(path string, at float64, side int) (image.Image, bool) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, false
	}
	dir, err := os.MkdirTemp("", "we-frame")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "frame.jpg")
	args := []string{
		"-y", "-ss", formatFFTime(at), "-i", path,
		"-frames:v", "1", "-update", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-1", side), out,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		// A timestamp past the end (a lying duration in the container) is
		// worth one retry at the very start of the clip.
		if at > 0 {
			return ffmpegFrameStart(path, side)
		}
		_ = b
		return nil, false
	}
	img, err := decodeImage(out)
	if err != nil {
		return nil, false
	}
	return img, true
}

func ffmpegFrameStart(path string, side int) (image.Image, bool) {
	dir, err := os.MkdirTemp("", "we-frame")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "frame.jpg")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"-y", "-i", path, "-frames:v", "1", "-update", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-1", side), out}
	if _, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return nil, false
	}
	img, err := decodeImage(out)
	if err != nil {
		return nil, false
	}
	return img, true
}

// pdftotext extracts PDF prose with poppler when it is installed. PDF has no
// pure-Go parser in this module, so the external tool is the difference
// between reading a report and looking at its cover.
func pdftotext(path string) (string, bool) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-enc", "UTF-8", "-nopgbrk", path, "-").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func formatClock(sec float64) string {
	d := time.Duration(sec * float64(time.Second)).Round(time.Second)
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func formatFFTime(sec float64) string {
	return fmt.Sprintf("%.3f", max(sec, 0))
}

func b64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }
