package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sunvale/internal/remote"
)

// remoteHandleDown implements GET/HEAD /d/*, the OpenList download endpoint:
// the local file behind the virtual path is streamed by the server itself,
// never through a redirect.
func (a *App) remoteHandleDown(w http.ResponseWriter, r *http.Request) {
	a.remoteServeDownload(w, r, "/d/")
}

// remoteHandleProxy implements GET/HEAD /p/*. For the local driver /p serves
// the same stream as /d, exactly as OpenList does for both endpoints.
func (a *App) remoteHandleProxy(w http.ResponseWriter, r *http.Request) {
	a.remoteServeDownload(w, r, "/p/")
}

// remoteServeDownload is the shared body of /d and /p: verify the sign, map
// the virtual path to disk and stream the file through http.ServeContent,
// which answers Range, HEAD and conditional requests.
func (a *App) remoteServeDownload(w http.ResponseWriter, r *http.Request, prefix string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		remote.HTMLFail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// r.URL.Path arrives percent-decoded. A bare "/d" is the root, "/d/".
	// The virtual path keeps its leading slash — the form remoteSign and
	// remoteVPath sign, and remoteDiskPath normalizes.
	rawPath := r.URL.Path
	if rawPath == strings.TrimSuffix(prefix, "/") {
		rawPath += "/"
	}
	vpath := "/" + strings.TrimSuffix(strings.TrimPrefix(rawPath, prefix), "/")
	sign := strings.TrimSuffix(r.URL.Query().Get("sign"), "/")
	// Directory-shaped requests (the root, or any trailing slash) need no
	// sign: they only ever end in a 500 page below. OpenList checks the sign
	// before touching the file, so file-shaped requests are verified first.
	dirShaped := vpath == "/" || strings.HasSuffix(rawPath, "/")
	if !dirShaped && !a.remoteVerifySign(vpath, sign) {
		remote.HTMLFail(w, http.StatusUnauthorized, remoteSignErrText(sign))
		return
	}
	d, inner, ok := a.remoteDriverFor(vpath)
	if !ok {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	o, err := d.Get(r.Context(), inner)
	if err != nil {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	if o.IsDir {
		remote.HTMLFail(w, http.StatusInternalServerError, "not a file")
		return
	}
	// A trailing slash on a real file is still a file download, so the sign
	// applies after all.
	if dirShaped && !a.remoteVerifySign(vpath, sign) {
		remote.HTMLFail(w, http.StatusUnauthorized, remoteSignErrText(sign))
		return
	}
	f, err := d.Open(r.Context(), inner)
	if err != nil {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	defer f.Close()

	name := o.Name
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=utf-8''%s`,
		url.PathEscape(name), url.PathEscape(name)))
	w.Header().Set("Etag", fmt.Sprintf(`"%x-%x"`, o.ModTime.Unix(), o.Size))
	http.ServeContent(w, r, name, o.ModTime, f)
}

// remoteSignErrText keeps OpenList's wording for a rejected sign: a sign
// whose trailing ":<unix expiry>" lies in the past is "sign expired",
// anything else (including a missing sign) is "sign invalid".
func remoteSignErrText(sign string) string {
	if i := strings.LastIndex(sign, ":"); i >= 0 {
		if exp, err := strconv.ParseInt(sign[i+1:], 10, 64); err == nil && exp != 0 && exp < time.Now().Unix() {
			return "sign expired"
		}
	}
	return "sign invalid"
}
