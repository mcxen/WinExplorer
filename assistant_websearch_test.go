package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	assistant "sunvale/internal/assistant"
)

// toolResultText flattens a result for gate-phrase assertions.
func toolResultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, ct := range res.Content {
		if t, ok := ct.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// decodeBody reads one JSON request body for the provider shape checks.
func decodeBody(r *http.Request, v any) {
	defer r.Body.Close()
	_ = json.NewDecoder(r.Body).Decode(v)
}

// assistant_websearch_test.go drives the web tools against mock
// providers: the gate without a key, both provider payload shapes, the
// skill toggle, and web_read's HTML-to-text cut.

func bindWebFor(t *testing.T, ws assistant.WebSearch, off bool) {
	t.Helper()
	sunvaleToolState.bindWeb(ws, off)
	t.Cleanup(func() { sunvaleToolState.bindWeb(assistant.WebSearch{}, false) })
	sunvaleToolState.bind(t.TempDir(), false, nil, nil, false)
}

// TestWebSearchGate: with nothing configured the tool says where to
// configure it, and the skill toggle says it is off.
func TestWebSearchGate(t *testing.T) {
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bindWebFor(t, assistant.WebSearch{}, false)
	res := callTool(t, sess, "web_search", map[string]any{"query": "Go 1.25"})
	text := toolResultText(res)
	if !res.IsError || !strings.Contains(text, "还没有配置") {
		t.Fatalf("unconfigured: %+v %s", res, text)
	}
	bindWebFor(t, assistant.WebSearch{Enabled: true, APIKey: "k"}, true)
	res = callTool(t, sess, "web_search", map[string]any{"query": "Go 1.25"})
	if text := toolResultText(res); !res.IsError || !strings.Contains(text, "在设置中关闭") {
		t.Fatalf("skill off: %+v %s", res, text)
	}
}

// TestWebSearchTavily: the Tavily shape goes out and its results come back
// as the tool's lines.
func TestWebSearchTavily(t *testing.T) {
	var gotAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = strings.Contains(r.Header.Get("Content-Type"), "application/json")
		var body map[string]any
		decodeBody(r, &body)
		if body["api_key"] != "tvly-test" || body["query"] != "天气" {
			http.Error(w, "bad request", 400)
			return
		}
		fmt.Fprint(w, `{"results":[{"title":"北京天气","url":"https://t.example/1","content":"晴转多云"}]}`)
	}))
	defer srv.Close()
	bindWebFor(t, assistant.WebSearch{Enabled: true, Provider: "tavily", APIKey: "tvly-test", BaseURL: srv.URL}, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "web_search", map[string]any{"query": "天气"})
	if !gotAuth || !strings.Contains(got, "北京天气") || !strings.Contains(got, "https://t.example/1") {
		t.Fatalf("tavily: %s", got)
	}
}

// TestWebSearchBocha: the 博查 shape goes out with a bearer key, and its
// webPages.value comes back as the tool's lines.
func TestWebSearchBocha(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bocha-key" {
			http.Error(w, "unauthorized", 401)
			return
		}
		fmt.Fprint(w, `{"code":200,"data":{"webPages":{"value":[{"name":"Go 官网","url":"https://go.dev","snippet":"The Go","summary":"Go 语言官网"}]}}}`)
	}))
	defer srv.Close()
	bindWebFor(t, assistant.WebSearch{Enabled: true, Provider: "bocha", APIKey: "bocha-key", BaseURL: srv.URL}, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "web_search", map[string]any{"query": "golang"})
	if !strings.Contains(got, "Go 官网") || !strings.Contains(got, "Go 语言官网") {
		t.Fatalf("bocha: %s", got)
	}
}

// TestWebReadCutsHTML: scripts and chrome go, the visible text stays.
func TestWebReadCutsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>t</title><style>body{color:red}</style></head>
<body><header>导航</header><article><h1>发布说明</h1><p>Go 1.25 发布了。</p></article>
<footer>页脚</footer><script>alert(1)</script></body></html>`)
	}))
	defer srv.Close()
	bindWebFor(t, assistant.WebSearch{Enabled: true, APIKey: "k"}, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "web_read", map[string]any{"url": srv.URL + "/page"})
	for _, want := range []string{"发布说明", "Go 1.25 发布了"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, drop := range []string{"alert(1)", "color:red", "导航", "页脚"} {
		if strings.Contains(got, drop) {
			t.Fatalf("%q leaked into %s", drop, got)
		}
	}
	res := callTool(t, sess, "web_read", map[string]any{"url": "ftp://x"})
	if text := toolResultText(res); !res.IsError || !strings.Contains(text, "http") {
		t.Fatalf("scheme gate: %+v %s", res, text)
	}
}

// decodeBody is defined once above; duplicate removed.
