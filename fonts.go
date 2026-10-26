package main

import (
	"embed"
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// fonts.go embeds the macOS 26 style's typefaces, all SIL OFL 1.1
// (fonts/OFL-*.txt). SF Pro may not be redistributed, so the style uses the
// nearest open faces: Inter (variable, opsz + wght, Latin subset) for UI
// text, JetBrains Mono (variable, Latin subset) for code, and Noto Sans CJK
// SC Regular/Bold subset to GB2312 (6763 hanzi + CJK punctuation) for
// Chinese. Glyphs outside the subsets fall back to the system fonts.

//go:embed fonts/*.ttf fonts/*.otf
var fontFS embed.FS

const (
	famUI   = "Sunvale Inter"
	famCJK  = "Sunvale CJK"
	famMono = "Sunvale Mono"
)

var (
	fontsOK   bool
	fontsOnce sync.Once
)

func registerFonts() {
	for _, f := range []struct{ file, fam string }{
		{"fonts/Inter.ttf", famUI},
		{"fonts/NotoSansSC-Regular.otf", famCJK},
		{"fonts/NotoSansSC-Bold.otf", famCJK},
		{"fonts/JetBrainsMono.ttf", famMono},
	} {
		b, err := fontFS.ReadFile(f.file)
		if err == nil {
			err = ui.RegisterFont(b, f.fam)
		}
		if err != nil {
			log.Printf("font %s: %v", f.file, err)
			return
		}
	}
	fontsOK = true
}

// uiFontFamily is the macOS 26 style's family list: the embedded faces,
// then the system's for anything they lack.
func uiFontFamily() string {
	if !fontsOK || systemFontPref {
		return "SF Pro Text, PingFang SC, Segoe UI Variable, Microsoft YaHei UI, Noto Sans CJK SC, system-ui"
	}
	return famUI + ", " + famCJK + ", PingFang SC, Microsoft YaHei UI, system-ui"
}

// systemFontPref is 使用系统字体 (appSettings.SystemFont).
var systemFontPref bool
