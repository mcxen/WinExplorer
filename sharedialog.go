package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sharedialog.go is the 分享 button's dialog: it turns the selection into an
// OpenList share and shows the link to hand out. The share itself is the
// server's (remote_share.go), reached here through its own handler rather
// than a second implementation — the same request the browser frontend
// sends, answered by the same code.

// shareDialog is the dialog's state while it is open.
type shareDialog struct {
	open bool
	// src are the selected disk paths, mapped to virtual paths on create.
	src []string

	usePwd bool
	pwdBuf string
	// expires is the chosen preset's label.
	expires string

	// result is the created share; err the failure to show instead.
	result *remoteShare
	err    string
	busy   bool
}

// shareExpiryOptions are the 有效期 presets, in the order shown.
var shareExpiryOptions = []struct {
	label string
	hours int
}{
	{"永久", 0},
	{"1 小时", 1},
	{"1 天", 24},
	{"7 天", 24 * 7},
	{"30 天", 24 * 30},
}

// shareDialogOpen opens the dialog for the current selection.
func (a *App) shareDialogOpen() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		a.statusMsg = "先选择要分享的文件"
		return
	}
	d := &a.share
	*d = shareDialog{open: true, expires: shareExpiryOptions[0].label}
	for _, e := range sel {
		d.src = append(d.src, e.Path)
	}
}

// remoteVPathOf maps a disk path back to the virtual path the share API
// takes: paths under the home directory become "/…", and everything else is
// refused, since the remote root is the home directory.
func remoteVPathOf(disk string) (string, bool) {
	home := filepath.Clean(remoteFSHome())
	p := filepath.Clean(disk)
	if p == home {
		return "/", true
	}
	rel, ok := strings.CutPrefix(p, home+string(filepath.Separator))
	if !ok {
		return "", false
	}
	return "/" + filepath.ToSlash(rel), true
}

// shareCreate builds the share through the server's own create handler. It
// runs on the UI thread: the work is local (a few stats and a store write),
// and running it inline keeps the dialog's frame free of a goroutine's
// result arriving mid-build.
func (a *App) shareCreate() {
	d := &a.share
	if !d.open || d.busy {
		return
	}
	files := make([]string, 0, len(d.src))
	for _, p := range d.src {
		vpath, ok := remoteVPathOf(p)
		if !ok {
			d.err = "只能分享主文件夹里的项目：" + filepath.Base(p)
			return
		}
		files = append(files, vpath)
	}

	body := map[string]any{"files": files}
	if d.usePwd {
		if pwd := strings.TrimSpace(d.pwdBuf); pwd != "" {
			body["pwd"] = pwd
		}
	}
	for _, o := range shareExpiryOptions {
		if o.label == d.expires && o.hours > 0 {
			body["expires"] = time.Now().Add(time.Duration(o.hours) * time.Hour).UTC().Format(time.RFC3339)
		}
	}

	share, code, msg := a.shareCallCreate(body)
	switch {
	case code != 200:
		if msg == "" {
			msg = "创建分享失败"
		}
		d.err = msg
	case share == nil:
		d.err = "创建分享失败：返回为空"
	default:
		d.result = share
		d.err = ""
	}
}

// shareCallCreate posts the body through remoteHandleShareCreate in-process,
// as the administrator, and decodes what it answers. Doing it this way means
// the dialog works whether or not the server is currently listening — the
// share is stored all the same — and needs no network round trip to
// localhost.
func (a *App) shareCallCreate(body map[string]any) (*remoteShare, int, string) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 400, err.Error()
	}
	req := httptest.NewRequest(http.MethodPost, "/api/share/create", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", a.remoteConf().Token)
	rec := httptest.NewRecorder()
	a.remoteHandleShareCreate(rec, req)

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		return nil, 500, err.Error()
	}
	if envelope.Code != 200 {
		return nil, envelope.Code, envelope.Message
	}
	var share remoteShare
	if err := json.Unmarshal(envelope.Data, &share); err != nil {
		return nil, 500, err.Error()
	}
	return &share, 200, ""
}

// shareLink is the address to hand out: the LAN URL with the share's /sd
// path.
func (a *App) shareLink() string {
	d := &a.share
	if d.result == nil {
		return ""
	}
	return strings.TrimSuffix(a.remoteLANURL(), "/") + "/sd/" + d.result.ID
}

// shareModal shows the dialog while it is open. Called from view.
func (a *App) shareModal(c *ui.Context) {
	if !a.share.open {
		return
	}
	floatDialog(c, &a.share.open, "分享", 440, false, func() {
		a.shareBody(c)
	})
}

// shareBody is the dialog's content: the files, the password and expiry,
// then the link once the share exists.
func (a *App) shareBody(c *ui.Context) {
	d := &a.share
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		names := make([]string, len(d.src))
		for i, p := range d.src {
			names[i] = filepath.Base(p)
		}
		ui.Text(c, "分享").FontSize(14).FontWeight(600).TextColor(colorText())
		ui.Text(c, "文件").FontSize(12).TextColor(colorMuted())
		ui.Text(c, strings.Join(names, "、")).FontSize(12).TextColor(colorText()).MaxLines(3)

		row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center)
		row.Children(func() {
			ui.Text(c, "访问密码").FontSize(12).TextColor(colorText()).Grow(1)
			v := d.usePwd
			if ui.Switch(c, &v).Changed() {
				d.usePwd = v
			}
		})
		if d.usePwd {
			ui.TextInputBase(c.Key("share-pwd"), &d.pwdBuf).
				FillWidth().Height(30).FontSize(13).Placeholder("留空则不设密码")
		}

		ui.Text(c, "有效期").FontSize(12).TextColor(colorMuted())
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			for _, o := range shareExpiryOptions {
				label := o.label
				btn := ui.ButtonBase(c).Height(28).Padding(0, 10).Radius(4).Label(label)
				if d.expires == label {
					btn.Background(colorSelected())
				} else if btn.Hovered() {
					btn.Background(colorHover())
				}
				btn.Children(func() {
					ui.Text(c, label).FontSize(12).TextColor(colorText())
				})
				if btn.Clicked() {
					d.expires = label
				}
			}
		})

		switch {
		case d.err != "":
			ui.Text(c, d.err).FontSize(12).TextColor(ui.Hex("#C4361D"))
		case d.result != nil:
			ui.Text(c, "分享链接").FontSize(12).TextColor(colorMuted())
			link := a.shareLink()
			ui.TextInputBase(c.Key("share-link"), &link).
				FillWidth().Height(30).FontSize(12)
			if d.result.Pwd != "" {
				ui.Text(c, "密码："+d.result.Pwd).FontSize(12).TextColor(colorMuted())
			}
		}

		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			uikitSecondaryButton(c, "关闭", func() { d.open = false })
			if d.result == nil {
				btn := ui.ButtonBase(c).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(4).
					Background(colorAccent()).Label("创建分享")
				btn.Children(func() {
					ui.Text(c, "创建分享").FontSize(12).TextColor(colorBtnText())
				})
				if btn.Clicked() {
					a.shareCreate()
				}
			} else {
				uikitPrimaryButton(c, "复制链接", func() {
					mygo.Clipboard.WriteText(a.shareLink())
					a.statusMsg = "分享链接已复制"
				})
			}
		})
	})
}
