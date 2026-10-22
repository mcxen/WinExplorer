package fileview

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// script.go runs the bundled Python extractor when Go cannot read a format
// on its own. Python is the "call a script to read it" route the assistant
// promises for video, PDF and the camera formats: the script itself only
// uses the standard library and takes whatever extra module the user has
// installed (Pillow, OpenCV, pypdf, python-docx, mutagen…), so a machine
// with more tools reads more files without a rebuild.

//go:embed extract.py
var extractScript []byte

// EnvExtractScript points at a replacement script. A user who wants their
// own reader (say, a company document store) swaps it here without
// recompiling; the JSON protocol stays the same.
const EnvExtractScript = "SUNVALE_FILEVIEW_PY"

const scriptTimeout = 45 * time.Second

// scriptPayload is the JSON the script prints. Fields mirror Result so the
// merge is a copy, not a translation.
type scriptPayload struct {
	OK     bool   `json:"ok"`
	Method string `json:"method"`
	Text   string `json:"text"`
	Trunc  bool   `json:"truncated"`
	Meta   struct {
		Duration   float64 `json:"duration"`
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		VideoCodec string  `json:"video_codec"`
		AudioCodec string  `json:"audio_codec"`
		Pages      int     `json:"pages"`
	} `json:"meta"`
	Frames []struct {
		Label  string `json:"label"`
		MIME   string `json:"mime"`
		Data   string `json:"data"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"frames"`
	Warnings []string `json:"warnings"`
}

// runScript asks the Python helper to read one file. ok is false when there
// is no interpreter, no script or the script failed: callers then try their
// own fallback rather than reporting an error.
func runScript(path string, opt Options, kind Kind) (scriptPayload, bool) {
	if opt.NoScript {
		return scriptPayload{}, false
	}
	bin, args := pythonCmd()
	if bin == "" {
		return scriptPayload{}, false
	}
	script := os.Getenv(EnvExtractScript)
	if script == "" {
		tmp, err := writeScript()
		if err != nil {
			return scriptPayload{}, false
		}
		script = tmp
	} else if _, err := os.Stat(script); err != nil {
		return scriptPayload{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), scriptTimeout)
	defer cancel()
	full := append(args, script, path,
		"--kind", string(kind),
		"--frames", fmt.Sprint(opt.MaxImages),
		"--side", fmt.Sprint(opt.ImageSide),
	)
	out, err := exec.CommandContext(ctx, bin, full...).Output()
	if err != nil {
		return scriptPayload{}, false
	}
	var payload scriptPayload
	if err := json.Unmarshal(bytes.TrimSpace(out), &payload); err != nil {
		return scriptPayload{}, false
	}
	if !payload.OK {
		return payload, false
	}
	return payload, true
}

// writeScript drops the embedded helper in the cache dir once per process.
// Embedding matters: an installed app has no source tree to read from.
var scriptOnce struct {
	path string
	err  error
}

func writeScript() (string, error) {
	if scriptOnce.path != "" || scriptOnce.err != nil {
		return scriptOnce.path, scriptOnce.err
	}
	dir, err := os.MkdirTemp("", "we-extract")
	if err != nil {
		scriptOnce.err = err
		return "", err
	}
	path := filepath.Join(dir, "extract.py")
	if err := os.WriteFile(path, extractScript, 0o700); err != nil {
		scriptOnce.err = err
		return "", err
	}
	scriptOnce.path = path
	return path, nil
}

// pythonCmd finds an interpreter. Windows ships "py" rather than
// "python3", and some systems only have "python".
func pythonCmd() (string, []string) {
	for _, cand := range []string{"python3", "python"} {
		if bin, err := exec.LookPath(cand); err == nil {
			return bin, nil
		}
	}
	if runtime.GOOS == "windows" {
		if bin, err := exec.LookPath("py"); err == nil {
			return bin, []string{"-3"}
		}
	}
	return "", nil
}

// mergeScript folds a script result into a Result, keeping frames already
// collected (Go's own extraction wins: it is cheaper and always present).
func mergeScript(res Result, out scriptPayload, opt Options) Result {
	if out.Method != "" {
		if res.Method == "" {
			res.Method = "Python 脚本（" + out.Method + "）"
		} else {
			res.Method += " + Python 脚本（" + out.Method + "）"
		}
	}
	if strings.TrimSpace(out.Text) != "" && strings.TrimSpace(res.Text) == "" {
		text, cut := clipRunes(tidyText(out.Text), opt.TextRunes)
		res.Text = text
		res.Truncated = out.Trunc || cut
	}
	if res.Meta.Duration == 0 {
		res.Meta.Duration = out.Meta.Duration
	}
	if res.Meta.Width == 0 {
		res.Meta.Width, res.Meta.Height = out.Meta.Width, out.Meta.Height
	}
	if res.Meta.VideoCodec == "" {
		res.Meta.VideoCodec = out.Meta.VideoCodec
	}
	if res.Meta.AudioCodec == "" {
		res.Meta.AudioCodec = out.Meta.AudioCodec
	}
	if res.Meta.Pages == 0 {
		res.Meta.Pages = out.Meta.Pages
	}
	res.Warnings = append(res.Warnings, out.Warnings...)
	for _, f := range out.Frames {
		if len(res.Frames) >= opt.MaxImages {
			break
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(f.Data, "data:image/png;base64,"))
		if err != nil || len(data) == 0 {
			continue
		}
		mime := f.MIME
		if mime == "" {
			mime = "image/png"
		}
		res.Frames = append(res.Frames, Frame{
			Label: f.Label, MIME: mime, Data: data, Width: f.Width, Height: f.Height,
		})
	}
	return res
}

// ExtractorStatus names the routes this machine can use, for the settings
// page: a user wondering why a video came back without frames wants to see
// "ffmpeg 未安装" next to the answer.
func ExtractorStatus() []string {
	var out []string
	if bin, _ := pythonCmd(); bin != "" {
		out = append(out, "Python 脚本可用（"+filepath.Base(bin)+"）")
	} else {
		out = append(out, "Python 未安装，脚本读取不可用")
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		out = append(out, "ffmpeg 可用（视频抽帧）")
	} else {
		out = append(out, "ffmpeg 未安装，视频无法抽帧")
	}
	if _, err := exec.LookPath("ffprobe"); err == nil {
		out = append(out, "ffprobe 可用（媒体元数据）")
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		out = append(out, "pdftotext 可用（PDF 正文）")
	} else {
		out = append(out, "pdftotext 未安装，PDF 依赖 Python 脚本或系统渲染")
	}
	if PosterFunc != nil {
		out = append(out, "系统渲染可用（缩略图兜底）")
	}
	return out
}
