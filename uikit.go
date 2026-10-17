package main

import (
	"github.com/egoist/mygo/ui"
)

// Shared dialog button styles, the "次要 + 主要" pair every dialog renders
// right-aligned at its foot. The atoms below are the same buttons for the
// irregular layouts; all colors come from theme.go's tokens.

// uikitSecondaryButton is the white-on-raised-surface button with a
// hairline border: 取消, 打开, 恢复默认, and friends.
func uikitSecondaryButton(c *ui.Context, label string, onClick func()) {
	btn := ui.ButtonBase(c).Height(30).Radius(4).
		Background(colorAddress()).Border(1, colorDivider()).Label(label)
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(colorText())
	})
	if btn.Clicked() && onClick != nil {
		onClick()
	}
}

// uikitPrimaryButton is the accent-filled button: 确定, 重命名, 关闭, 检查更新.
// Its label uses colorBtnText — white on the light accent, near-black on
// the dark one, where white reads poorly.
func uikitPrimaryButton(c *ui.Context, label string, onClick func()) {
	btn := ui.ButtonBase(c).Height(30).Radius(4).
		Background(colorAccent()).Label(label)
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(colorBtnText())
	})
	if btn.Clicked() && onClick != nil {
		onClick()
	}
}

// uikitDialogButtons renders the standard right-aligned 取消 + 主要 pair.
// Call it inside a row the caller lays out, e.g.:
//
//	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
//		uikitDialogButtons(c, "确定", onOK, onCancel)
//	})
//
// Buttons needing a third action (恢复默认) or different labels keep their
// own row and use the atoms above.
func uikitDialogButtons(c *ui.Context, primaryLabel string, onPrimary func(), onCancel func()) {
	uikitSecondaryButton(c, "取消", onCancel)
	uikitPrimaryButton(c, primaryLabel, onPrimary)
}
