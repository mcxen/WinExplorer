package main

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/internal/assistant"
)

// assistant_openui_media.go paints OpenUI MediaList (OPENUI-MEDIA): one row
// per item, a rounded thumbnail card on the left — picture, video poster
// with the play badge, document page or the file glyph — and the title and
// body on the right, rows split by a hairline. A click on the card opens
// the 空格 quick preview for that file.

const (
	openMediaThumbW   float32 = 136
	openMediaThumbH   float32 = 92
	openMediaFetchMax         = 16 << 20
)

func (a *App) paintOpenMediaList(c *ui.Context, v *ouiView) {
	ui.Column(c).FillWidth().MinWidth(0).Label("OpenUI media list").Children(func() {
		if v.Text != "" {
			ui.Text(c.Key("title"), v.Text).FontSize(14).FontWeight(600).TextColor(colorText()).
				Wrap().Selectable().Padding(0, 0, 6, 0)
		}
		for i, it := range v.Items {
			if i > 0 {
				ui.Box(c.Key("div" + itoa(i))).FillWidth().Height(1).Background(colorDivider())
			}
			a.paintOpenMediaRow(c.Key("row"+itoa(i)), v, it, i)
		}
	})
}

func (a *App) paintOpenMediaRow(c *ui.Context, list, it *ouiView, i int) {
	ui.Row(c).FillWidth().MinWidth(0).Gap(12).Padding(8, 0).AlignItems(ui.Start).Children(func() {
		a.paintOpenMediaThumb(c.Key("thumb"), it)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			title := it.Text
			if n := assistant.OpenMediaNumber(list, it, i); n != "" {
				title = n + ". " + title
			}
			if strings.TrimSpace(title) != "" {
				ui.Text(c.Key("t"), title).FontSize(13).FontWeight(600).TextColor(colorText()).Wrap().Selectable()
			}
			if it.Sub != "" {
				ui.Text(c.Key("b"), it.Sub).FontSize(12).LineHeight(1.45).TextColor(colorText()).Wrap().Selectable()
			}
		})
	})
}

func (a *App) paintOpenMediaThumb(c *ui.Context, it *ouiView) {
	local := openMediaLocal(it.Path)
	card := ui.ButtonBase(c).Size(openMediaThumbW, openMediaThumbH).Shrink(0).Radius(8).Clip().
		Border(1, colorDivider()).Background(colorAddress()).Label("预览 " + baseName(it.Path))
	card.Children(func() {
		var bm *ui.Bitmap
		switch {
		case strings.HasPrefix(it.Path, "data:image/"):
			bm, _ = formLoadBitmap(it.Path, 256)
		case local != "":
			if it.Variant == "image" && !isImageExt(lowerExt(local)) {
				bm, _ = formLoadBitmap(local, 256)
			} else {
				bm = mediaThumb(local, 256)
			}
		}
		if bm != nil {
			fit := ui.Cover
			if it.Variant == "doc" {
				// A page reads whole: letterboxed on the card, not cropped
				// to its blank middle.
				fit = ui.Contain
			}
			ui.Image(c, bm).Fit(fit).Size(openMediaThumbW, openMediaThumbH)
		} else {
			ui.Box(c).Size(openMediaThumbW, openMediaThumbH).Center().Children(func() {
				a.fileIcon(c, Entry{Ext: lowerExt(openMediaURLPath(it.Path))}, 40)
			})
		}
		if it.Variant == "video" {
			a.videoBadge(c, openMediaThumbH)
		}
	})
	if card.Hovered() {
		card.Border(1, colorAccent().Alpha(0.6))
	}
	components.Tooltip(c, card, baseName(openMediaURLPath(it.Path)))
	if card.Clicked() && local != "" {
		a.openQuickLookPath(local)
	}
}

// openMediaURLPath strips a URL's query so the extension reads.
func openMediaURLPath(src string) string {
	if strings.Contains(src, "://") {
		if i := strings.IndexAny(src, "?#"); i >= 0 {
			return src[:i]
		}
	}
	return src
}

// openMediaLocal is the file a MediaItem draws from: the path itself, a
// file:// URL's path, or for http(s) the cached download ("" until the
// background fetch lands).
func openMediaLocal(src string) string {
	src = strings.TrimSpace(src)
	switch {
	case src == "", strings.HasPrefix(src, "data:"):
		return ""
	case strings.HasPrefix(src, "file://"):
		return strings.TrimPrefix(src, "file://")
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		return openMediaFetch(src)
	}
	return src
}

var (
	openMediaFetchMu   sync.Mutex
	openMediaFetched   = map[string]string{} // url -> local path, "" failed
	openMediaFetchHook func(url string) (io.ReadCloser, error)
)

// openMediaFetch downloads a remote picture once into the temp cache, off
// the frame; the first frames draw the glyph and the repaint swaps it in.
func openMediaFetch(url string) string {
	openMediaFetchMu.Lock()
	p, ok := openMediaFetched[url]
	if !ok {
		openMediaFetched[url] = ""
	}
	openMediaFetchMu.Unlock()
	if ok {
		return p
	}
	go func() {
		local := openMediaDownload(url)
		openMediaFetchMu.Lock()
		openMediaFetched[url] = local
		openMediaFetchMu.Unlock()
		if local != "" {
			repaint()
		}
	}()
	return ""
}

func openMediaDownload(url string) string {
	sum := sha1.Sum([]byte(url))
	ext := filepath.Ext(openMediaURLPath(url))
	if len(ext) > 6 {
		ext = ""
	}
	dir := filepath.Join(os.TempDir(), "sunvale-openui-media")
	local := filepath.Join(dir, hex.EncodeToString(sum[:8])+ext)
	if st, err := os.Stat(local); err == nil && st.Size() > 0 {
		return local
	}
	var body io.ReadCloser
	if openMediaFetchHook != nil {
		b, err := openMediaFetchHook(url)
		if err != nil {
			return ""
		}
		body = b
	} else {
		cl := &http.Client{Timeout: 20 * time.Second}
		resp, err := cl.Get(url)
		if err != nil {
			return ""
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return ""
		}
		body = resp.Body
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, openMediaFetchMax+1))
	if err != nil || len(data) == 0 || len(data) > openMediaFetchMax {
		return ""
	}
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(local, data, 0o644) != nil {
		return ""
	}
	return local
}

// openQuickLookPath opens the quick preview on a file that is not the
// selection — a MediaList card. Closing the overlay drops the override.
func (a *App) openQuickLookPath(path string) {
	if path == "" {
		return
	}
	a.quickLookFile = path
	a.quickLook = true
	a.requestPreview()
}
