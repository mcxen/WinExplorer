package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// appicon.go 显示应用自己的图标：macOS 的 .app 读 Contents/Info.plist 的
// CFBundleIconFile 并解码 .icns 里的 PNG 条目；Linux 的 .desktop 按 Icon=
// 找绝对路径或 hicolor/pixmaps 里的 PNG；Windows 的 .exe/.lnk 走 Shell
// （appicon_windows.go）。解码在 thumb.go 的后台工作池里做，结果按路径+修改
// 时间缓存、受同一预算约束；视图构建只查缓存，从不读盘。解不出的退回绘制的
// 通用应用图标。

// appIconThumb is e's own application icon, or nil while it is decoding or
// when it has none.
func appIconThumb(e Entry, px float32) *ui.Bitmap {
	if e.Path == "" || !filepath.IsAbs(e.Path) {
		return nil
	}
	side := thumbSide(px)
	key := thumbCacheKey("a:" + e.Path + "@" + strconv.FormatInt(e.ModTime.UnixNano(), 36))
	path, dir := e.Path, e.IsDir
	return cachedThumb(key, side, func() image.Image {
		return loadAppIcon(path, dir, side)
	})
}

// loadAppIcon reads the icon of the application at path, scaled to at most
// side px, or nil. It blocks; callers run it off the UI thread.
func loadAppIcon(path string, dir bool, side int) image.Image {
	var img image.Image
	switch {
	case dir && isAppBundleName(path):
		img = macAppIcon(path, side)
	case lowerExt(path) == "desktop":
		img = desktopEntryIcon(path, side)
	default:
		img = platformAppIcon(path, side)
	}
	if img == nil {
		return nil
	}
	return fitSide(img, side)
}

// fitSide scales img down so its longer edge is at most side.
func fitSide(img image.Image, side int) image.Image {
	b := img.Bounds()
	if b.Dx() <= side && b.Dy() <= side {
		return img
	}
	w, h := side, side
	if b.Dx() > b.Dy() {
		h = max(1, b.Dy()*side/b.Dx())
	} else {
		w = max(1, b.Dx()*side/b.Dy())
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// macAppIcon is a .app bundle's icon from its Info.plist and .icns.
func macAppIcon(app string, side int) image.Image {
	res := filepath.Join(app, "Contents", "Resources")
	name := ""
	if data, err := readSmall(filepath.Join(app, "Contents", "Info.plist"), 1<<20); err == nil {
		name = plistString(data, "CFBundleIconFile")
	}
	var cands []string
	if name != "" {
		name = filepath.Base(name)
		if !strings.HasSuffix(strings.ToLower(name), ".icns") {
			cands = append(cands, name+".icns")
		}
		cands = append(cands, name)
	}
	cands = append(cands, "AppIcon.icns")
	for _, c := range cands {
		if data, err := readSmall(filepath.Join(res, c), 16<<20); err == nil {
			if img := decodeICNS(data, side); img != nil {
				return img
			}
		}
	}
	// No named icon: the bundle's only .icns, when it has exactly one.
	ents, _ := os.ReadDir(res)
	var only string
	for _, d := range ents {
		if strings.HasSuffix(strings.ToLower(d.Name()), ".icns") {
			if only != "" {
				return nil
			}
			only = d.Name()
		}
	}
	if only != "" {
		if data, err := readSmall(filepath.Join(res, only), 16<<20); err == nil {
			return decodeICNS(data, side)
		}
	}
	return nil
}

func readSmall(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > limit || st.IsDir() {
		return nil, os.ErrInvalid
	}
	buf := make([]byte, st.Size())
	_, err = io.ReadFull(f, buf)
	return buf, err
}

var pngSig = []byte("\x89PNG\r\n\x1a\n")

// decodeICNS picks the smallest PNG entry at least side px wide (else the
// largest) from an .icns file. JPEG 2000 and the legacy RLE entries are not
// decoded; a file holding only those yields nil.
func decodeICNS(data []byte, side int) image.Image {
	if len(data) < 8 || string(data[:4]) != "icns" {
		return nil
	}
	end := min(int(binary.BigEndian.Uint32(data[4:8])), len(data))
	var best []byte
	bestW := 0
	for off := 8; off+8 <= end; {
		n := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		if n < 8 || off+n > end {
			break
		}
		body := data[off+8 : off+n]
		off += n
		if !bytes.HasPrefix(body, pngSig) {
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			continue
		}
		w := cfg.Width
		better := best == nil ||
			(w >= side && (bestW < side || w < bestW)) ||
			(w < side && bestW < side && w > bestW)
		if better {
			best, bestW = body, w
		}
	}
	if best == nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(best))
	if err != nil {
		return nil
	}
	return img
}

// plistString is the string value of a top-level key in an XML or binary
// property list, or "".
func plistString(data []byte, key string) string {
	if bytes.HasPrefix(data, []byte("bplist00")) {
		return bplistString(data, key)
	}
	re := regexp.MustCompile(`<key>\s*` + regexp.QuoteMeta(key) + `\s*</key>\s*<string>([^<]*)</string>`)
	if m := re.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// bplistString reads one string value from a binary plist's top-level
// dictionary: just enough of the format for Info.plist lookups.
func bplistString(d []byte, key string) string {
	if len(d) < 40 {
		return ""
	}
	t := d[len(d)-32:]
	offSize, refSize := int(t[6]), int(t[7])
	num := int(binary.BigEndian.Uint64(t[8:16]))
	top := int(binary.BigEndian.Uint64(t[16:24]))
	table := int(binary.BigEndian.Uint64(t[24:32]))
	if offSize < 1 || offSize > 8 || refSize < 1 || refSize > 8 || num <= 0 || num > 1<<20 || table < 0 || table+num*offSize > len(d) {
		return ""
	}
	be := func(b []byte) int {
		v := 0
		for _, c := range b {
			v = v<<8 | int(c)
		}
		return v
	}
	offset := func(obj int) int {
		if obj < 0 || obj >= num {
			return -1
		}
		o := be(d[table+obj*offSize : table+(obj+1)*offSize])
		if o >= len(d) {
			return -1
		}
		return o
	}
	// count reads a marker's length, following the extended int form.
	count := func(o int) (n, start int) {
		n = int(d[o] & 0x0F)
		start = o + 1
		if n == 0x0F {
			if start >= len(d) {
				return -1, 0
			}
			sz := 1 << (d[start] & 0x0F)
			if start+1+sz > len(d) {
				return -1, 0
			}
			n = be(d[start+1 : start+1+sz])
			start += 1 + sz
		}
		return n, start
	}
	str := func(obj int) string {
		o := offset(obj)
		if o < 0 {
			return ""
		}
		n, s := count(o)
		if n < 0 {
			return ""
		}
		switch d[o] >> 4 {
		case 0x5:
			if s+n <= len(d) {
				return string(d[s : s+n])
			}
		case 0x6:
			if s+2*n <= len(d) {
				u := make([]uint16, n)
				for i := range u {
					u[i] = binary.BigEndian.Uint16(d[s+2*i:])
				}
				return string(utf16.Decode(u))
			}
		}
		return ""
	}
	o := offset(top)
	if o < 0 || d[o]>>4 != 0xD {
		return ""
	}
	n, s := count(o)
	if n < 0 || s+2*n*refSize > len(d) {
		return ""
	}
	for i := 0; i < n; i++ {
		k := be(d[s+i*refSize : s+(i+1)*refSize])
		if str(k) == key {
			return str(be(d[s+(n+i)*refSize : s+(n+i+1)*refSize]))
		}
	}
	return ""
}

// desktopEntryIcon resolves a .desktop file's Icon=: an absolute PNG path,
// or a name looked up in the hicolor theme and pixmaps. SVG-only and
// themed icons beyond that stay with the generic glyph.
func desktopEntryIcon(path string, side int) image.Image {
	data, err := readSmall(path, 256<<10)
	if err != nil {
		return nil
	}
	icon := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Icon="); ok {
			icon = strings.TrimSpace(v)
			break
		}
	}
	if icon == "" {
		return nil
	}
	var cands []string
	if filepath.IsAbs(icon) {
		cands = append(cands, icon)
	} else if !strings.ContainsAny(icon, `/\`) {
		home, _ := os.UserHomeDir()
		roots := []string{filepath.Join(home, ".local/share/icons/hicolor"), "/usr/share/icons/hicolor"}
		for _, r := range roots {
			for _, sz := range []string{"256x256", "128x128", "96x96", "64x64", "48x48"} {
				cands = append(cands, filepath.Join(r, sz, "apps", icon+".png"))
			}
		}
		cands = append(cands, "/usr/share/pixmaps/"+icon+".png")
	}
	for _, c := range cands {
		if !strings.HasSuffix(strings.ToLower(c), ".png") {
			continue
		}
		if data, err := readSmall(c, 8<<20); err == nil {
			if img, err := png.Decode(bytes.NewReader(data)); err == nil {
				return img
			}
		}
	}
	return nil
}
