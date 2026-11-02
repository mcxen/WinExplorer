package main

// theme_contrast_test.go pins the light/dark sweep's colored spots to the
// theme tokens. The share dialog, the extension editor and the batch rename
// dialog all painted their error lines with a hardcoded light-mode red, and
// the OpenUI step dots and the video quick-look's CTA drew white on a fill
// that is a light accent in dark mode — the same "颜色只用令牌" rule BUGSCAN-8
// fixed in the assistant panel. withDark lives in assistant_theme_test.go.

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// maxRed returns the strongest red channel among clearly red opaque pixels.
// The theme's danger red is the only red in the rendered surfaces, and its
// red channel separates the light token (196) from the dark one (255): a
// hardcoded light-mode red stays at 196 in both appearances.
func maxRed(img image.Image) uint8 {
	var best uint8
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA()
			if pa>>8 < 200 {
				continue
			}
			cr, cg, cb := uint8(pr>>8), uint8(pg>>8), uint8(pb>>8)
			if int(cr) <= int(cg)+25 || int(cr) <= int(cb)+25 || cr < 120 {
				continue
			}
			if cr > best {
				best = cr
			}
		}
	}
	return best
}

// textLuma returns the darkest and brightest opaque pixel luminance inside the
// text's own laid-out rect, the ink and its background. The rect is sampled
// exactly: a padded window would reach the step rail's connector line or the
// pane behind a pill and skew the extremes.
func textLuma(img image.Image, r ui.Rect) (minL, maxL int) {
	minL, maxL = 999, -1
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA()
			if pa>>8 < 128 {
				continue
			}
			l := (int(pr>>8)*299 + int(pg>>8)*587 + int(pb>>8)*114) / 1000
			if l < minL {
				minL = l
			}
			if l > maxL {
				maxL = l
			}
		}
	}
	return minL, maxL
}

// assertDangerRedFollowsTheme renders a surface with an error line in both
// appearances and checks the red is the theme's danger token: the dark token
// is much brighter than the light one, while a hardcoded red would not move.
func assertDangerRedFollowsTheme(t *testing.T, render func(dark bool) image.Image) {
	t.Helper()
	lr, dr := maxRed(render(false)), maxRed(render(true))
	if lr == 0 || dr == 0 {
		t.Fatalf("no red error pixels: light=%d dark=%d", lr, dr)
	}
	if dr <= lr || dr < 230 {
		t.Fatalf("dark error red %d (light %d) is not the theme's danger token", dr, lr)
	}
}

// TestShareDialogErrorRedFollowsTheme covers sharedialog.go's error line.
func TestShareDialogErrorRedFollowsTheme(t *testing.T) {
	render := func(dark bool) image.Image {
		withDark(t, dark)
		a := &App{}
		a.share = shareDialog{open: true, expires: shareExpiryOptions[0].label, err: "只能分享主文件夹里的项目：/etc"}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Children(func() { a.shareBody(c) })
		}, 440, 320)
		tt.Frame()
		if !tt.HasText("只能分享主文件夹里的项目：/etc") {
			t.Fatalf("share error line not drawn: %v", tt.Texts())
		}
		return tt.Image()
	}
	assertDangerRedFollowsTheme(t, render)
}

// TestExtEditorErrorRedFollowsTheme covers settings_ext.go's validation line.
func TestExtEditorErrorRedFollowsTheme(t *testing.T) {
	render := func(dark bool) image.Image {
		withDark(t, dark)
		a := &App{}
		a.extEdit = extEditState{open: true, index: -1}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Children(func() { a.extEditBody(c) })
		}, 440, 360)
		tt.Frame()
		if !tt.HasText("名称、匹配规则和命令都填写后才能确定") {
			t.Fatalf("extension validation line not drawn: %v", tt.Texts())
		}
		return tt.Image()
	}
	assertDangerRedFollowsTheme(t, render)
}

// TestBatchRenameErrorRedFollowsTheme covers batchrename.go's clash notice and
// per-row error names.
func TestBatchRenameErrorRedFollowsTheme(t *testing.T) {
	render := func(dark bool) image.Image {
		withDark(t, dark)
		a, tt, _ := keynavWindow(t, 3)
		a.selectAll()
		a.doRenameStart()
		a.batch.pattern = ".."
		a.batchReplan()
		if !a.batchHasClash() {
			t.Fatal("the clash pattern did not flag the rows")
		}
		tt.Frame()
		for i := 0; i < 4; i++ {
			tt.Advance(120 * time.Millisecond)
		}
		tt.Frame()
		return tt.Image()
	}
	assertDangerRedFollowsTheme(t, render)
}

// TestOpenUIStepDotFollowsTheme: a done/active/error step dot fills itself with
// a theme token (success/accent/danger) and draws its glyph on that fill, so
// the glyph must take the on-accent token — white in light mode, near-black in
// dark mode. A hardcoded white would leave the dark dot's glyph washed out.
func TestOpenUIStepDotFollowsTheme(t *testing.T) {
	render := func(dark bool) (image.Image, ui.Rect) {
		withDark(t, dark)
		a := &App{}
		text := "```openui\nroot = Steps([Step(\"扫描\", \"完成\", \"done\"), Step(\"清理\", \"\", \"active\")], \"进度\")\n```"
		a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
		tt := assistTester(a, 500)
		tt.Frame()
		tt.Frame()
		r, ok := tt.Find("2") // the active step's number, drawn inside the dot
		if !ok {
			t.Fatalf("active step dot not drawn: %v", tt.Texts())
		}
		return tt.Image(), r
	}
	darkImg, darkRect := render(true)
	if darkMin, _ := textLuma(darkImg, darkRect); darkMin > 80 {
		t.Fatalf("dark step dot glyph is too light on its fill: min luminance %d", darkMin)
	}
	lightImg, lightRect := render(false)
	if _, lightMax := textLuma(lightImg, lightRect); lightMax < 220 {
		t.Fatalf("light step dot glyph is too dark on its fill: max luminance %d", lightMax)
	}
}

// TestVideoOpenButtonFollowsTheme: the video quick-look's CTA is an accent-filled
// button, so its label takes the on-accent token rather than hardcoded white.
func TestVideoOpenButtonFollowsTheme(t *testing.T) {
	render := func(dark bool) (image.Image, ui.Rect) {
		withDark(t, dark)
		a := &App{}
		st := &previewState{path: "clip.mp4"}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Children(func() {
				a.videoQuickOpenButton(c, st)
			})
		}, 320, 64)
		tt.Frame()
		r, ok := tt.Find("用系统播放器打开")
		if !ok {
			t.Fatalf("the video button label is not drawn: %v", tt.Texts())
		}
		return tt.Image(), r
	}
	darkImg, darkRect := render(true)
	if darkMin, _ := textLuma(darkImg, darkRect); darkMin > 80 {
		t.Fatalf("video button label too light on the dark accent: min luminance %d", darkMin)
	}
	lightImg, lightRect := render(false)
	if _, lightMax := textLuma(lightImg, lightRect); lightMax < 220 {
		t.Fatalf("video button label too dark on the light accent: max luminance %d", lightMax)
	}
}
