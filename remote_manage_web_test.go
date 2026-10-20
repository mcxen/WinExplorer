package main

import (
	"regexp"
	"strings"
	"testing"
)

// remote_manage_web_test.go guards the shape of the built-in management page
// (remote_manage_web.html). The page is one self-contained document — no
// build step, no external asset — and its files view mirrors the desktop
// shell: the command bar, the address row, the six view modes, the preview
// pane, the status bar and the drag-and-drop handlers all have to be there,
// and every element id the script reaches for has to exist. Serving it is
// covered by TestRemoteManagePage; the Go side of the fs and archive
// endpoints by remote_manage_test.go and remote_archive_test.go.

var (
	manageIDRe  = regexp.MustCompile(`id="([A-Za-z0-9_-]+)"`)
	manageRefRe = regexp.MustCompile(`\$\('([A-Za-z0-9_-]+)'\)`)
	manageExtRe = regexp.MustCompile(`(?i)(src|href)\s*=\s*['"]?(https?:)?//`)
)

func TestRemoteManagePageIntegrity(t *testing.T) {
	page := remoteManageHTML
	if !strings.Contains(page, "<!DOCTYPE html>") || !strings.Contains(page, "</html>") {
		t.Fatal("the embedded management page is not a whole document")
	}

	// One file, no external assets: a single document with inline styles
	// and an inline script, exactly as remote_manage_web.go embeds it.
	for _, bad := range []string{"<script src", "<link ", "<style src", `src="http`, `src="//`, `href="http`, `href="//`} {
		if strings.Contains(page, bad) {
			t.Errorf("the page must stay self-contained, found %q", bad)
		}
	}
	if m := manageExtRe.FindString(page); m != "" {
		t.Errorf("the page links out to %q", m)
	}

	// Every $('id') the script uses must name an element that exists: a
	// typo there fails silently in a browser.
	ids := map[string]bool{}
	for _, m := range manageIDRe.FindAllStringSubmatch(page, -1) {
		ids[m[1]] = true
	}
	refs := manageRefRe.FindAllStringSubmatch(page, -1)
	if len(refs) < 50 {
		t.Fatalf("only %d element lookups found — is the script still inline?", len(refs))
	}
	for _, m := range refs {
		if !ids[m[1]] {
			t.Errorf("$('%s') has no matching element", m[1])
		}
	}

	// The files view's skeleton, all of it aligned with the desktop shell.
	for _, id := range []string{
		// 命令栏
		"cmdbar", "cbNew", "cbCut", "cbCopy", "cbPaste", "cbRename", "cbShare",
		"cbDel", "cbSort", "cbView", "cbMore", "cbUpload", "fileIn",
		// 地址行
		"addrbar", "navBack", "navFwd", "navUp", "navRefresh", "crumbs", "filterBox",
		// 地址栏的编辑框与下拉（Windows 式）
		"addrEditBtn", "addrWrap", "addrEdit", "addrDrop",
		// 主体与状态栏
		"fzone", "previewpane", "statusbar", "statusLeft", "statusMode",
		// 导航树、上传进度与拖拽提示
		"navtree", "uplist", "dropHint",
		// 存储页：根目录与挂载点
		"pg-storage", "rootPath", "rootSave", "rootNow", "storageList",
		"stMount", "stRoot", "stAdd",
	} {
		if !ids[id] {
			t.Errorf("the files view lost #%s", id)
		}
	}

	// The command bar's wording and order come from the desktop, the
	// archive commands from the remote endpoints behind them.
	for _, want := range []string{
		"新建", "剪切", "复制", "粘贴", "重命名", "分享", "删除", "排序", "查看", "上传",
		"超大图标", "大图标", "中等图标", "小图标", "列表", "详细信息",
		"紧凑视图", "显示隐藏的项目", "显示项目复选框", "预览窗格",
		"压缩为 ZIP 文件", "全部解压", "解压到…", "复制为路径", "属性",
		"历史记录", "补全建议", "清除历史记录", "输入路径",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the management page lost the %q command", want)
		}
	}

	// Selection, keyboard and drag-and-drop behave like the window does.
	for _, want := range []string{
		"api/fs/archive/zip", "api/fs/archive/unzip", "api/public/archive_extensions",
		"/api/fs/'+op", "api/fs/remove", // move 与 copy 共用 batchOp 拼出的路径
		"dragstart", "dragover", "dataTransfer",
		"F2", "F5", "Backspace", "Alt+P", "sunvale_view", "sunvale_sort",
		// 地址栏的历史与快捷键
		"sunvale_addr_hist", "Ctrl+L", "Alt+D", "api/fs/dirs",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the management page lost %q", want)
		}
	}

	// The phone layout: bottom tab bar, single-column cards, touch-sized
	// targets and the touch gestures (tap to open, long-press menu, swipe
	// between previews) that replace double-click and right-click.
	for _, want := range []string{
		"viewport-fit=cover", "env(safe-area-inset-bottom)",
		"(hover:none) and (pointer:coarse)",
		"touchstart", "touchend", "bindLongPress", "bindPreviewSwipe",
		"dvh", "grid-template-areas",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the management page lost its phone adaptation: %q", want)
		}
	}
}
