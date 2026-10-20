package remote

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
)

// HTMLFail writes the plain HTML error page of the /d and /p
// endpoints: the status line as title and heading, the message as the body
// paragraph, like OpenList's error pages.
func HTMLFail(w http.ResponseWriter, code int, msg string) {
	title := strconv.Itoa(code)
	if text := http.StatusText(code); text != "" {
		title = fmt.Sprintf("%d %s", code, text)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		title, title, html.EscapeString(msg))
}
