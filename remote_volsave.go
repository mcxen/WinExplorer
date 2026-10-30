package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"sunvale/internal/remote"
)

// remote_volsave.go is the web side of 图片快存 (volsave.go) plus the
// page's web app manifest. Every endpoint needs the administrator login;
// the target and the source must both map into the remote's own roots.

type remoteVolSaveResp struct {
	Name string `json:"name,omitempty"`
	Dir  string `json:"dir"`
	Dup  bool   `json:"dup,omitempty"`
	Msg  string `json:"msg,omitempty"`
}

// remoteVolSaveTargetDisk is the configured target when the web may write
// there: it maps back through the remote's roots to the same folder.
func (a *App) remoteVolSaveTargetDisk() (disk, vpath string, ok bool) {
	disk = volSaveTarget()
	if disk == "" {
		return "", "", false
	}
	vpath = remoteVPath(disk)
	back, ok := a.remoteDiskPath(vpath)
	if !ok || filepath.Clean(back) != filepath.Clean(disk) {
		return disk, "", false
	}
	return disk, vpath, true
}

// remoteHandleVolSaveTarget answers GET (current target) and POST {dir}.
func (a *App) remoteHandleVolSaveTarget(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Dir string `json:"dir"`
		}
		if err := remote.ReadReq(r, &req); err != nil {
			remote.Err(w, 400, "bad request")
			return
		}
		disk := ""
		if strings.TrimSpace(req.Dir) != "" {
			d, ok := a.remoteDiskPath(req.Dir)
			if !ok || !a.remoteWriteGuard(w, req.Dir) {
				if !ok {
					remote.Err(w, 403, "目录不在可访问范围")
				}
				return
			}
			disk = d
		}
		if err := setVolSaveTarget(disk); err != nil {
			remote.Err(w, 400, err.Error())
			return
		}
	}
	_, vp, _ := a.remoteVolSaveTargetDisk()
	remote.OK(w, remoteVolSaveResp{Dir: vp})
}

// remoteHandleVolSave answers POST {path}: copy that image into the target.
func (a *App) remoteHandleVolSave(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	disk, vdir, ok := a.remoteVolSaveTargetDisk()
	if disk == "" {
		remote.Err(w, 400, errVolSaveNoTarget.Error())
		return
	}
	if !ok {
		remote.Err(w, 403, "快存目录不在可访问范围")
		return
	}
	if !a.remoteWriteGuard(w, vdir) {
		return
	}
	src, ok := a.remoteDiskPath(req.Path)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	dst, dup, err := volSaveCopy(src, disk)
	if err != nil {
		remote.Err(w, 400, err.Error())
		return
	}
	remote.OK(w, remoteVolSaveResp{Name: filepath.Base(dst), Dir: vdir, Dup: dup, Msg: volSaveMsg(dup, nil)})
}

// remoteHandleVolSaveUndo answers POST: take back the last save's copy.
func (a *App) remoteHandleVolSaveUndo(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	if r.Method != http.MethodPost {
		remote.Err(w, 405, "method not allowed")
		return
	}
	dst, err := volSaveUndo()
	if err != nil {
		remote.Err(w, 409, err.Error())
		return
	}
	remote.OK(w, remoteVolSaveResp{Name: filepath.Base(dst), Msg: "已取消"})
}

const remoteManifest = `{"name":"晴谷","short_name":"晴谷","start_url":"/","scope":"/","display":"standalone","background_color":"#101010","theme_color":"#101010","icons":[{"src":"/icon-192.png","sizes":"192x192","type":"image/png","purpose":"any"},{"src":"/icon-512.png","sizes":"512x512","type":"image/png","purpose":"any"}]}`

func (a *App) remoteHandleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write([]byte(remoteManifest))
}

var remoteIconCache sync.Map

// remoteHandleIcon draws the app icon (a rounded blue tile with a white
// folder tab) at 192 or 512 px, so no binary asset ships in the repo.
func (a *App) remoteHandleIcon(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/icon-"), ".png"))
	if n != 192 && n != 512 {
		http.NotFound(w, r)
		return
	}
	if b, ok := remoteIconCache.Load(n); ok {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b.([]byte))
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	bg := color.RGBA{0x25, 0x63, 0xeb, 0xff}
	fg := color.RGBA{0xff, 0xff, 0xff, 0xff}
	rad := n / 5
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			cx, cy := min(max(x, rad), n-1-rad), min(max(y, rad), n-1-rad)
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy > rad*rad {
				continue
			}
			c := bg
			fx, fy := x*16/n, y*16/n
			if (fy >= 5 && fy < 12 && fx >= 3 && fx < 13) || (fy == 4 && fx >= 3 && fx < 7) {
				c = fg
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	remoteIconCache.Store(n, buf.Bytes())
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}
