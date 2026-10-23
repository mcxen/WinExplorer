package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// docview.go is the document Space quick look (DOCPREV1): PDF and Office
// files are rasterised a few pages at a time and shown side by side in the
// overlay, with ‹ › (or PageUp/PageDown) flipping pages. PDF goes through
// pdfthumb.go's pdftoppm path (Quick Look first page on macOS when poppler
// is missing). Office files are converted to PDF with LibreOffice
// (soffice --headless) once per file version and cached in the temp dir;
// without LibreOffice the overlay degrades to a placeholder and the system
// open button. Enter / double-click still hand the file to the OS.

const (
	// docPagesPerView is how many pages sit side by side in the overlay.
	docPagesPerView = 3
	// docMaxPages caps how far the overlay rasterises; past it the user is
	// pointed at the system app.
	docMaxPages = 30
	// docPageSide is the longest-edge pixel size of a rasterised page.
	docPageSide = 900
	// docConvertTimeout bounds one LibreOffice conversion.
	docConvertTimeout = 40 * time.Second
)

// officeExts are the formats that need LibreOffice to become a PDF.
var officeExts = map[string]bool{
	"doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true,
	"odt": true, "ods": true, "odp": true, "rtf": true,
}

func isOfficeExt(ext string) bool { return officeExts[strings.ToLower(ext)] }

// sofficeBin finds LibreOffice's CLI, or "" when it is not installed.
// Tests override it.
var sofficeBin = func() string {
	for _, name := range []string{"soffice", "libreoffice"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/Applications/LibreOffice.app/Contents/MacOS/soffice",
		`C:\Program Files\LibreOffice\program\soffice.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// errNoOffice is the soft miss when an Office file cannot be converted.
var errNoOffice = fmt.Errorf("未安装 LibreOffice，无法在应用内预览 Office 文档")

var officeConvMu sync.Mutex

// docPDFSource returns a PDF for path: the file itself for a PDF, or a
// cached LibreOffice conversion for an Office file keyed on path+size+mtime.
func docPDFSource(path string) (string, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "pdf" {
		return path, nil
	}
	if !isOfficeExt(ext) {
		return "", fmt.Errorf("不支持的文档格式")
	}
	bin := sofficeBin()
	if bin == "" {
		return "", errNoOffice
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	dir := filepath.Join(os.TempDir(), "sunvale-docprev", hex.EncodeToString(sum[:8]))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	out := filepath.Join(dir, base+".pdf")
	if fi, err := os.Stat(out); err == nil && fi.Size() > 0 {
		return out, nil
	}
	// One conversion at a time: LibreOffice shares a user profile and a
	// second headless instance would fail on the lock.
	officeConvMu.Lock()
	defer officeConvMu.Unlock()
	if fi, err := os.Stat(out); err == nil && fi.Size() > 0 {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	profile := filepath.Join(os.TempDir(), "sunvale-docprev", "lo-profile")
	ctx, cancel := context.WithTimeout(context.Background(), docConvertTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--headless", "--norestore",
		"-env:UserInstallation=file://"+filepath.ToSlash(profile),
		"--convert-to", "pdf", "--outdir", dir, path)
	if b, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("LibreOffice 转换超时")
		}
		return "", fmt.Errorf("LibreOffice 转换失败：%s", strings.TrimSpace(string(b)))
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		return "", fmt.Errorf("LibreOffice 未生成 PDF")
	}
	return out, nil
}

var pdfPagesRe = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

// pdfPageCount asks pdfinfo for the page count; 0 when unknown.
func pdfPageCount(pdf string) int {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), pdfThumbTimeout)
	defer cancel()
	b, err := exec.CommandContext(ctx, "pdfinfo", pdf).Output()
	if err != nil {
		return 0
	}
	if m := pdfPagesRe.FindSubmatch(b); m != nil {
		n, _ := strconv.Atoi(string(m[1]))
		return n
	}
	return 0
}

// pdfPageImage rasterises one 1-based page with pdftoppm. Page 1 falls
// back to pdfPosterImage (Quick Look on macOS) when poppler is missing.
func pdfPageImage(pdf string, page, side int) (image.Image, error) {
	if page <= 1 {
		return pdfPosterImage(pdf, side)
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, fmt.Errorf("需要 pdftoppm 才能翻页")
	}
	dir, err := os.MkdirTemp("", "we-pdfpage")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "p")
	ctx, cancel := context.WithTimeout(context.Background(), 2*pdfThumbTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page),
		"-singlefile", "-scale-to", strconv.Itoa(side), pdf, prefix)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %s", strings.TrimSpace(string(b)))
	}
	f, err := os.Open(prefix + ".png")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// docQuickView is the per-window state of the document overlay.
type docQuickView struct {
	path    string
	ver     string // size+mtime, so an edited file re-rasterises
	pdf     string
	total   int // page count; 0 unknown
	start   int // 0-based first page shown
	pages   map[int]*ui.Bitmap
	failed  map[int]bool
	loading bool
	err     string
}

// docPageWindow returns the clamped first page and the page indexes
// (0-based) visible from start.
func docPageWindow(start, total int) (int, []int) {
	limit := total
	if limit <= 0 {
		limit = 1
	}
	if limit > docMaxPages {
		limit = docMaxPages
	}
	maxStart := limit - docPagesPerView
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	var idx []int
	for i := start; i < start+docPagesPerView && i < limit; i++ {
		idx = append(idx, i)
	}
	return start, idx
}

// resetDocQV rebinds the overlay to path, seeding page 1 from the pane's
// poster so the first frame is never blank.
func (a *App) resetDocQV(path string, poster *ui.Bitmap) {
	ver := docVersion(path)
	if a.docQV.path == path && a.docQV.ver == ver {
		return
	}
	a.docQV = docQuickView{path: path, ver: ver, pages: map[int]*ui.Bitmap{}, failed: map[int]bool{}}
	if poster != nil {
		a.docQV.pages[0] = poster
	}
	a.loadDocPages()
}

// docVersion identifies one revision of path on disk.
func docVersion(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d|%d", fi.Size(), fi.ModTime().UnixNano())
}

// flipDoc moves the page window by dir views.
func (a *App) flipDoc(dir int) {
	s, _ := docPageWindow(a.docQV.start+dir*docPagesPerView, a.docQV.total)
	if s == a.docQV.start {
		return
	}
	a.docQV.start = s
	a.loadDocPages()
}

// loadDocPages rasterises whatever visible page is missing, off the main
// thread; a newer flip or file supersedes it via docGen.
func (a *App) loadDocPages() {
	gen := atomic.AddUint64(&a.docGen, 1)
	q := a.docQV
	_, want := docPageWindow(q.start, max(q.total, docPagesPerView))
	if q.pdf != "" {
		_, want = docPageWindow(q.start, q.total)
	}
	var missing []int
	for _, i := range want {
		if q.pages[i] == nil && !q.failed[i] {
			missing = append(missing, i)
		}
	}
	if q.pdf != "" && len(missing) == 0 {
		return
	}
	have := map[int]bool{}
	for i, b := range q.pages {
		if b != nil {
			have[i] = true
		}
	}
	a.docQV.loading = true
	path := q.path
	go func() {
		pdf, total := q.pdf, q.total
		var srcErr error
		if pdf == "" {
			pdf, srcErr = docPDFSource(path)
			if srcErr == nil {
				total = pdfPageCount(pdf)
				if total <= 0 {
					total = 1
				}
				_, want = docPageWindow(q.start, total)
				missing = missing[:0]
				for _, i := range want {
					if !have[i] {
						missing = append(missing, i)
					}
				}
			}
		}
		got := map[int]*ui.Bitmap{}
		bad := map[int]bool{}
		if srcErr == nil {
			for _, i := range missing {
				if atomic.LoadUint64(&a.docGen) != gen {
					return
				}
				if img, err := pdfPageImage(pdf, i+1, docPageSide); err == nil && img != nil {
					got[i] = ui.NewBitmap(img)
				} else {
					bad[i] = true
				}
			}
		}
		apply := func() {
			if a.docQV.path != path {
				return
			}
			if atomic.LoadUint64(&a.docGen) == gen {
				a.docQV.loading = false
			}
			if srcErr != nil {
				a.docQV.err = srcErr.Error()
			} else {
				a.docQV.pdf, a.docQV.total = pdf, total
			}
			for i, b := range got {
				a.docQV.pages[i] = b
			}
			for i := range bad {
				a.docQV.failed[i] = true
			}
			repaint()
		}
		if mygo.App.IsReady() {
			mygo.RunOnMain(apply)
		} else {
			apply()
		}
	}()
}

// docQuickKeys: PageUp/PageDown flip pages; ←/→ stay on adjacent files.
func (a *App) docQuickKeys(c *ui.Context) {
	if c.Shortcut(0, ui.KeyPageDown) {
		a.flipDoc(1)
	}
	if c.Shortcut(0, ui.KeyPageUp) {
		a.flipDoc(-1)
	}
}

// docQuickContent is the Space overlay body for PDF/Office.
func (a *App) docQuickContent(c *ui.Context, st *previewState, maxW, maxH float32) {
	a.resetDocQV(st.path, st.bitmap)
	a.docQuickKeys(c)
	q := &a.docQV
	start, idx := docPageWindow(q.start, max(q.total, 1))
	if q.total == 0 && q.err == "" {
		idx = []int{0}
	}
	if q.err != "" && q.pages[0] == nil {
		a.docUnavailable(c, st, q.err)
		return
	}
	pageH := maxH - 90
	if pageH < 160 {
		pageH = 160
	}
	pageW := (maxW - 120 - float32(docPagesPerView-1)*16) / docPagesPerView
	if pageW > pageH*0.75 {
		pageW = pageH * 0.75
	}
	ui.Row(c).Width(maxW).Gap(16).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		a.docFlipButton(c, "‹", "上一组页面（PageUp）", start > 0, func() { a.flipDoc(-1) })
		for _, i := range idx {
			a.docPageCard(c, i, pageW, pageH)
		}
		_, last := docPageWindow(start+docPagesPerView, max(q.total, 1))
		canNext := len(last) > 0 && last[len(last)-1] > idx[len(idx)-1]
		a.docFlipButton(c, "›", "下一组页面（PageDown）", canNext, func() { a.flipDoc(1) })
	})
	info := "正在栅格化…"
	if q.total > 0 {
		end := idx[len(idx)-1] + 1
		info = fmt.Sprintf("第 %d–%d 页 / 共 %d 页", idx[0]+1, end, q.total)
		if q.total > docMaxPages {
			info += fmt.Sprintf("（应用内预览前 %d 页）", docMaxPages)
		}
	}
	if q.err != "" {
		info = q.err
	}
	ui.Text(c, info).FontSize(12).TextColor(colorMuted())
	a.quickLookOpenButton(c, st)
	ui.Text(c, "首页栅格预览 · PageUp/PageDown 翻页 · ←/→ 相邻文件 · 回车 系统默认打开 · 空格/Esc 关闭").
		FontSize(11).TextColor(colorMuted())
}

func (a *App) docFlipButton(c *ui.Context, glyph, tip string, enabled bool, on func()) {
	b := ui.ButtonBase(c).Size(32, 32).Radius(16).Shrink(0).Label(tip).Tooltip(tip).
		Background(colorAddress()).Border(1, colorDivider())
	col := colorText()
	if !enabled {
		col = colorMuted().Alpha(0.4)
	}
	b.Children(func() { ui.Text(c, glyph).FontSize(18).TextColor(col) })
	if enabled && b.Clicked() {
		on()
	}
}

func (a *App) docPageCard(c *ui.Context, i int, w, h float32) {
	bm := a.docQV.pages[i]
	card := ui.Box(c).Width(w).Height(h).Shrink(0).Radius(4).Clip().
		Background(ui.RGBA(255, 255, 255, 1)).Border(1, colorDivider())
	card.Children(func() {
		if bm != nil {
			ui.Image(c, bm).Fit(ui.Contain)
			return
		}
		msg := "正在渲染…"
		if a.docQV.failed[i] {
			msg = "此页无法渲染"
		}
		ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Text(c, msg).FontSize(12).TextColor(ui.RGBA(120, 120, 120, 1))
		})
	})
}

// docUnavailable is the graceful degrade: icon, reason, system open.
func (a *App) docUnavailable(c *ui.Context, st *previewState, msg string) {
	ui.Column(c).Gap(12).AlignItems(ui.Center).Padding(32, 16).Children(func() {
		ui.Box(c).Size(96, 96).Shrink(0).Children(func() {
			a.fileIcon(c, Entry{Ext: stExt(st)}, 96)
		})
		ui.Text(c, msg).FontSize(13).TextColor(colorMuted()).TextAlign(ui.Center)
		ui.Text(c, "可点击下方按钮或按回车，用系统默认应用打开").FontSize(12).TextColor(colorMuted())
		a.quickLookOpenButton(c, st)
	})
}

// docPoster is the sidebar first-page thumbnail for PDF and Office files.
func docPoster(path string) (*ui.Bitmap, error) {
	pdf, err := docPDFSource(path)
	if err != nil {
		if b, e := quickLookThumb(path, 512); e == nil {
			return b, nil
		}
		return nil, err
	}
	return pdfPoster(pdf)
}

// monoFont is the fixed-width stack for text/code previews.
const monoFont = "SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace"

// expandTabs widens tabs for the read-only text area, which draws them
// as nothing, so code keeps its indentation in the monospace preview.
func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }
