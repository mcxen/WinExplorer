package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeMirrors(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, defaultUpdateMirrors},
		{[]string{" https://a.example ", "DIRECT", "http://insecure/", "https://a.example/"}, []string{"https://a.example/", mirrorDirect}},
		{splitMirrors("https://x.example/\r\ndirect,"), []string{"https://x.example/", mirrorDirect}},
	}
	for _, c := range cases {
		if got := normalizeMirrors(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("normalizeMirrors(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// rewriteRT 把所有请求改发到测试服务器，记录原始地址。
type rewriteRT struct {
	srv  *httptest.Server
	seen []string
}

func (r *rewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.seen = append(r.seen, req.Method+" "+req.URL.String())
	nr := req.Clone(req.Context())
	nr.URL.Scheme, nr.URL.Host = "http", strings.TrimPrefix(r.srv.URL, "http://")
	nr.URL.Path = "/" + req.URL.Host + req.URL.Path
	nr.URL.RawPath = ""
	return http.DefaultTransport.RoundTrip(nr)
}

func TestMirrorTransportFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Location", "https://github.com/o/r/releases/tag/v9.9.9")
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/bad.example/"):
			http.Error(w, "boom", http.StatusBadGateway)
		case strings.HasPrefix(r.URL.Path, "/good.example/") && strings.Contains(r.URL.Path, "/download/v9.9.9/update.json"):
			io.WriteString(w, "manifest")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rt := &rewriteRT{srv: srv}
	mt := newMirrorTransport(rt)
	mt.mirrors = func() []string { return []string{"https://bad.example/", "https://good.example/", mirrorDirect} }
	c := &http.Client{Transport: mt}
	resp, err := c.Get("https://github.com/o/r/releases/latest/download/update.json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "manifest" {
		t.Fatalf("body %q, seen %q", b, rt.seen)
	}

	// 全部失败时返回错误，不会把失败的响应交给更新器。
	mt.mirrors = func() []string { return []string{"https://bad.example/"} }
	if _, err := c.Get("https://github.com/o/r/releases/download/v1/a.tar.gz"); err == nil {
		t.Fatal("want error when every mirror fails")
	}
	// 非 release 地址不走镜像。
	rt.seen = nil
	resp, err = c.Get("https://example.com/x")
	if err == nil {
		resp.Body.Close()
	}
	if len(rt.seen) != 1 || rt.seen[0] != "GET https://example.com/x" {
		t.Fatalf("passthrough seen %q", rt.seen)
	}
}
