package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 自动更新的镜像换线。
//
// mygo 的更新器（github.com/egoist/mygo updater.go）用 http.DefaultClient
// 拉取 https://github.com/<repo>/releases/latest/download/update-<平台>.json，
// 再从清单里的 github.com/.../releases/download/<tag>/... 下载安装包和 delta，
// 下载完成后用编进程序的 ed25519 公钥（mygo.json 的 updates.publicKey）校验
// 签名。这里只替换传输层：请求 github.com 的 release 文件时按镜像列表顺序换线
// 重试，每条线有超时。内容仍由 mygo 校验签名，镜像返回的任何东西没有正确签名
// 都会被拒绝，所以镜像不能绕过校验。

// mirrorDirect 表示直连 github.com。
const mirrorDirect = "direct"

// defaultUpdateMirrors 是未配置时的换线顺序：先走 tvv.tw 代理，再直连。
var defaultUpdateMirrors = []string{"https://tvv.tw/", mirrorDirect}

// mirrorAttemptTimeout 是每条线拿到响应头的时限；之后的 body 读取不受它限制。
var mirrorAttemptTimeout = 20 * time.Second

// updateMirrorList 返回当前生效的镜像列表，由 main 接到设置上。
var updateMirrorList = func() []string { return defaultUpdateMirrors }

// normalizeMirrors 清理配置：去空白、去重，代理前缀补成以 / 结尾，
// 只接受 https 前缀；结果为空时回落到默认值。
func normalizeMirrors(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if !strings.EqualFold(m, mirrorDirect) {
			if !strings.HasPrefix(m, "https://") {
				continue
			}
			if !strings.HasSuffix(m, "/") {
				m += "/"
			}
		} else {
			m = mirrorDirect
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultUpdateMirrors...)
	}
	return out
}

// releaseFileRE 匹配 github.com 上的 release 下载地址。
var releaseFileRE = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/releases/(latest/download|download/[^/]+)/[^/]+$`)

// latestTagRE 取 releases/latest 跳转地址里的 tag。
var latestTagRE = regexp.MustCompile(`/releases/tag/([^/?#]+)$`)

// mirrorTransport 把 github.com 的 release 下载按镜像顺序重试，其它请求原样转发。
type mirrorTransport struct {
	base    http.RoundTripper
	mirrors func() []string

	mu        sync.Mutex
	latestTag map[string]string // repo -> 最近解析到的 latest tag
}

func newMirrorTransport(base http.RoundTripper) *mirrorTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &mirrorTransport{base: base, mirrors: func() []string { return updateMirrorList() }}
}

func (t *mirrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw := req.URL.String()
	m := releaseFileRE.FindStringSubmatch(raw)
	if m == nil || req.Method != http.MethodGet {
		return t.base.RoundTrip(req)
	}
	var errs []error
	for _, mirror := range normalizeMirrors(t.mirrors()) {
		target := raw
		if mirror != mirrorDirect {
			// 代理不跟随 latest/download 的跳转（tvv.tw 会 502），
			// 先解析成具体 tag 的直链再走代理。
			if m[2] == "latest/download" {
				tag, err := t.resolveLatest(req.Context(), m[1])
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", mirror, err))
					continue
				}
				target = strings.Replace(raw, "/releases/latest/download/", "/releases/download/"+tag+"/", 1)
			}
			target = mirror + target
		}
		start := time.Now()
		resp, err := t.attempt(req, target)
		if err == nil {
			slog.Info("updater mirror ok", "mirror", mirror, "url", redactURL(target), "ms", time.Since(start).Milliseconds())
			return resp, nil
		}
		slog.Warn("updater mirror failed, trying next", "mirror", mirror, "url", redactURL(target), "err", err, "ms", time.Since(start).Milliseconds())
		errs = append(errs, fmt.Errorf("%s: %w", mirror, err))
	}
	slog.Error("updater: all mirrors failed", "err", errors.Join(errs...))
	return nil, fmt.Errorf("所有更新线路均失败：%w", errors.Join(errs...))
}

// attempt 发起一次请求：响应头必须在时限内到达，且状态为 200。
func (t *mirrorTransport) attempt(req *http.Request, target string) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(mirrorAttemptTimeout, cancel)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		timer.Stop()
		cancel()
		return nil, err
	}
	r.Header = req.Header.Clone()
	resp, err := t.followRedirects(r)
	if !timer.Stop() && err == nil {
		resp.Body.Close()
		cancel()
		return nil, context.DeadlineExceeded
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("GET %s: %s", target, resp.Status)
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// followRedirects 在 transport 层跟随跳转（RoundTripper 本身不跟随），
// 只允许 https。
func (t *mirrorTransport) followRedirects(r *http.Request) (*http.Response, error) {
	for i := 0; i < 10; i++ {
		resp, err := t.base.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		loc := resp.Header.Get("Location")
		if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
			return resp, nil
		}
		resp.Body.Close()
		next, err := r.URL.Parse(loc)
		if err != nil {
			return nil, err
		}
		if next.Scheme != "https" && !(next.Scheme == "http" && r.URL.Scheme == "http") {
			return nil, fmt.Errorf("拒绝跳转到非 HTTPS 地址 %s", next)
		}
		nr := r.Clone(r.Context())
		nr.URL = next
		nr.Host = ""
		r = nr
	}
	return nil, errors.New("跳转次数过多")
}

// resolveLatest 不跟随跳转地请求 releases/latest，从 Location 取 tag。
// 失败时沿用上次解析到的 tag。
func (t *mirrorTransport) resolveLatest(ctx context.Context, repo string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, mirrorAttemptTimeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURLBase+repo+"/releases/latest", nil)
	if err == nil {
		var resp *http.Response
		resp, err = t.base.RoundTrip(r)
		if err == nil {
			resp.Body.Close()
			if mm := latestTagRE.FindStringSubmatch(resp.Header.Get("Location")); mm != nil {
				t.mu.Lock()
				if t.latestTag == nil {
					t.latestTag = map[string]string{}
				}
				t.latestTag[repo] = mm[1]
				t.mu.Unlock()
				return mm[1], nil
			}
			err = fmt.Errorf("releases/latest: %s", resp.Status)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if tag := t.latestTag[repo]; tag != "" {
		return tag, nil
	}
	return "", err
}

// latestURLBase 可在测试中替换。
var latestURLBase = "https://github.com/"

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// installUpdateMirrors 把镜像换线装到 http.DefaultClient（mygo 更新器用它）。
func installUpdateMirrors(store *persistedState) {
	updateMirrorList = func() []string { return normalizeMirrors(splitMirrors(store.settings().UpdateMirrors)) }
	http.DefaultClient.Transport = newMirrorTransport(http.DefaultClient.Transport)
}

// splitMirrors 把设置里逗号或换行分隔的镜像串拆成列表。
func splitMirrors(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ';' })
}
