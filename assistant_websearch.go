package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/net/html"

	assistant "sunvale/internal/assistant"
)

// assistant_websearch.go is the assistant's way out to the public web: a
// web_search tool backed by a configurable provider (Tavily, or 博查 for a
// Chinese index), and a web_read tool that turns a page's HTML into plain
// text for the model. The key and endpoint live in assistant.json's
// prefs.webSearch, bound per run like the archive passwords; with no key
// the tools stay on the registry but answer with where to configure them,
// so a model's call fails obvious instead of unexpected.

// webHTTP is the web tools' client. One per process, with a ceiling that
// keeps a wedged endpoint from stalling a run forever.
var webHTTP = &http.Client{Timeout: 30 * time.Second}

// webSearchTimeout caps one search or read inside the run's own ctx.
const webSearchTimeout = 20 * time.Second

// webBodyMax is the most of a page web_read pulls in, webTextMax the most
// of it the model sees.
const (
	webBodyMax = 2 << 20
	webTextMax = 40 * 1024
)

func (s *sunvaleTools) bindWeb(ws assistant.WebSearch, disabled bool) {
	s.mu.Lock()
	s.web, s.webOff = ws, disabled
	s.mu.Unlock()
}

// bindFrequent gives the run's frequent_files its snapshot of the usage
// ledger, as bindWeb does for the search providers.
func (s *sunvaleTools) bindFrequent(paths []string) {
	s.mu.Lock()
	s.frequent = append([]string(nil), paths...)
	s.mu.Unlock()
}

func (s *sunvaleTools) webConf() (assistant.WebSearch, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.web, s.webOff
}

type webSearchArgs struct {
	Query string `json:"query" jsonschema:"搜索词"`
	Max   int    `json:"max,omitempty" jsonschema:"返回条数，默认 5，最多 10"`
}

// webHit is one result line in provider-independent form.
type webHit struct {
	Title, URL, Snippet string
}

func (s *sunvaleTools) webSearch(ctx context.Context, _ *mcp.CallToolRequest, args webSearchArgs) (*mcp.CallToolResult, any, error) {
	ws, off, err := s.webReady()
	if err != nil {
		return mcpErr(err), nil, nil
	}
	_ = off
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return mcpErr(fmt.Errorf("query 不能为空")), nil, nil
	}
	max := args.Max
	if max <= 0 {
		max = 5
	}
	if max > 10 {
		max = 10
	}
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	var hits []webHit
	switch ws.Provider {
	case "", "tavily":
		hits, err = tavilySearch(ctx, ws, query, max)
	case "bocha":
		hits, err = bochaSearch(ctx, ws, query, max)
	default:
		err = fmt.Errorf("未知的网页搜索供应商 %q（支持 tavily、bocha）", ws.Provider)
	}
	if err != nil {
		return mcpErr(err), nil, nil
	}
	if len(hits) == 0 {
		return mcpText("没有搜到结果。"), nil, nil
	}
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "%d. %s\n%s\n%s\n\n", i+1, h.Title, h.URL, h.Snippet)
		if b.Len() > webTextMax {
			b.WriteString("（其余结果已省略）\n")
			break
		}
	}
	b.WriteString("要读某个链接的正文，用 web_read。")
	return mcpText(b.String()), nil, nil
}

// webReady is both web tools' gate: off by skill, or not configured.
func (s *sunvaleTools) webReady() (assistant.WebSearch, bool, error) {
	ws, off := s.webConf()
	switch {
	case off:
		return ws, off, fmt.Errorf("网页搜索已在设置中关闭")
	case !ws.Enabled || strings.TrimSpace(ws.APIKey) == "":
		return ws, off, fmt.Errorf("网页搜索还没有配置：设置 → 助手 → 网页搜索，填入供应商和密钥")
	}
	return ws, off, nil
}

// tavilySearch posts Tavily's one-request search shape.
func tavilySearch(ctx context.Context, ws assistant.WebSearch, query string, max int) ([]webHit, error) {
	endpoint := strings.TrimSpace(ws.BaseURL)
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}
	body, _ := json.Marshal(map[string]any{
		"api_key": ws.APIKey, "query": query, "max_results": max,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := webHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Tavily 返回 %s：%s", res.Status, webSnippet(res.Body))
	}
	var out struct {
		Results []struct {
			Title, URL, Content string
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, webBodyMax)).Decode(&out); err != nil {
		return nil, err
	}
	hits := make([]webHit, 0, len(out.Results))
	for _, r := range out.Results {
		hits = append(hits, webHit{Title: r.Title, URL: r.URL, Snippet: r.Content})
	}
	return hits, nil
}

// bochaSearch posts 博查's web-search shape: bearer key, results under
// data.webPages.value.
func bochaSearch(ctx context.Context, ws assistant.WebSearch, query string, max int) ([]webHit, error) {
	endpoint := strings.TrimSpace(ws.BaseURL)
	if endpoint == "" {
		endpoint = "https://api.bochaai.com/v1/web-search"
	}
	body, _ := json.Marshal(map[string]any{"query": query, "count": max, "summary": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ws.APIKey)
	res, err := webHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			WebPages struct {
				Value []struct {
					Name, URL, Snippet, Summary string
				} `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, webBodyMax)).Decode(&out); err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("博查返回 %s：%s", res.Status, strings.TrimSpace(out.Msg))
	}
	hits := make([]webHit, 0, len(out.Data.WebPages.Value))
	for _, v := range out.Data.WebPages.Value {
		snippet := v.Summary
		if snippet == "" {
			snippet = v.Snippet
		}
		hits = append(hits, webHit{Title: v.Name, URL: v.URL, Snippet: snippet})
	}
	return hits, nil
}

// webSnippet is a short body peek for an error line.
func webSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return strings.TrimSpace(string(b))
}

type webReadArgs struct {
	URL string `json:"url" jsonschema:"要读取的网址，http 或 https"`
}

// webRead fetches one page plain: HTML shrinks to its visible text, and a
// text response passes through. Binary types and redirects to non-http
// schemes are refused.
func (s *sunvaleTools) webRead(ctx context.Context, _ *mcp.CallToolRequest, args webReadArgs) (*mcp.CallToolResult, any, error) {
	if _, _, err := s.webReady(); err != nil {
		return mcpErr(err), nil, nil
	}
	u := strings.TrimSpace(args.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return mcpErr(fmt.Errorf("url 需要以 http:// 或 https:// 开头")), nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; sunvale-assistant/1.0)")
	res, err := webHTTP.Do(req)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return mcpErr(fmt.Errorf("%s 返回 %s", u, res.Status)), nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, webBodyMax))
	if err != nil {
		return mcpErr(err), nil, nil
	}
	ctype := res.Header.Get("Content-Type")
	var text string
	if strings.Contains(ctype, "html") {
		text = webHTMLText(body)
	} else if strings.Contains(ctype, "text/") || strings.Contains(ctype, "json") || ctype == "" {
		text = string(body)
	} else {
		return mcpErr(fmt.Errorf("不支持的内容类型 %q（只读网页和文本）", ctype)), nil, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return mcpText(u + "\n（页面没有可读正文）"), nil, nil
	}
	if len(text) > webTextMax {
		text = string([]rune(text)[:webTextMax/4]) + "\n（正文过长，已截断）"
	}
	return mcpText(u + "\n" + text), nil, nil
}

// webHTMLText keeps a page's visible words: scripts, styles and head
// chrome go, the rest flattens to collapsed whitespace.
func webHTMLText(b []byte) string {
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return string(b)
	}
	var out strings.Builder
	var walk func(n *html.Node, skip bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "iframe", "svg", "nav", "footer", "header":
				skip = true
			case "p", "br", "div", "li", "tr", "h1", "h2", "h3", "h4", "article", "section":
				out.WriteByte('\n')
			}
		}
		if n.Type == html.TextNode && !skip {
			out.WriteString(n.Data)
			out.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}
	walk(doc, false)
	// Structural newlines split paragraphs; within one, runs of space
	// collapse. Empty lines go.
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if s := strings.Join(strings.Fields(line), " "); s != "" {
			lines = append(lines, s)
		}
	}
	return strings.Join(lines, "\n")
}

// registerWebSearchTools puts the web tools on the shared registry.
func registerWebSearchTools(s *sunvaleTools, server *mcp.Server) {
	addSunvaleTool(s, server, &mcp.Tool{
		Name:        "web_search",
		Description: "联网搜索网页（需要在 设置 → 助手 里配置网页搜索）。问到最新信息、资料、文档、新闻时调用。",
	}, s.webSearch)
	addSunvaleTool(s, server, &mcp.Tool{
		Name:        "web_read",
		Description: "读取一个网址的正文。web_search 得到链接后用它看全文；二进制内容不支持。",
	}, s.webRead)
}
