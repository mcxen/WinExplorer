package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// assistant_http.go is the admin-only HTTP MCP control surface for the
// assistant (list/save/activate/delete providers, run a headless turn).
// A2A lives in assistant_a2a.go. Both share the same in-process sunvale
// tools as the pane and never return API keys, archive-password values,
// cookies, tokens, or private-key material. Split for maintainability (LAY2).

// assistantHTTPServer is the outer MCP listener. Stateless mode gives each
// call a fresh protocol session, so a client can reconnect without losing
// configuration or file-state, which lives on disk and in sunvaleToolState.
func (a *App) assistantHTTPServer() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "sunvale-assistant", Version: remoteAdminVersion}, nil)
	a.registerAssistantControlTools(server)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.remoteIsAdmin(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

type assistantListArgs struct{}

type assistantSaveArgs struct {
	ID        string `json:"id,omitempty" jsonschema:"已有供应商的 id；省略时新建或按名称更新"`
	Name      string `json:"name" jsonschema:"供应商名称"`
	BaseURL   string `json:"baseURL" jsonschema:"OpenAI 兼容接口地址"`
	Model     string `json:"model" jsonschema:"模型名"`
	APIKey    string `json:"apiKey,omitempty" jsonschema:"新密钥；编辑时留空保留原密钥"`
	Active    bool   `json:"active,omitempty" jsonschema:"保存后是否设为当前供应商"`
	Autopilot *bool  `json:"autopilot,omitempty" jsonschema:"全托管默认开关"`
}

type assistantActivateArgs struct {
	ID string `json:"id" jsonschema:"要激活的供应商 id"`
}

type assistantDeleteArgs struct {
	ID string `json:"id" jsonschema:"要删除的供应商 id"`
}

type assistantRunArgs struct {
	Prompt    string                 `json:"prompt" jsonschema:"要求助手做的事"`
	Folder    string                 `json:"folder,omitempty" jsonschema:"远程虚拟文件夹，默认 /"`
	Files     []string               `json:"files,omitempty" jsonschema:"指定读取的文件路径，最多 12 个"`
	Autopilot bool                   `json:"autopilot,omitempty" jsonschema:"本轮全托管"`
	History   []assistantHistoryTurn `json:"history,omitempty" jsonschema:"可选的历史消息"`
}

type assistantHistoryTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func (a *App) registerAssistantControlTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_config_list", Description: "列出助手供应商和当前配置；不会返回密钥或密码。"},
		func(context.Context, *mcp.CallToolRequest, assistantListArgs) (*mcp.CallToolResult, any, error) {
			disk, err := loadAssistantConfig()
			if err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpJSON(assistantConfigStatusOf(disk)), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_config_save", Description: "新建或更新助手供应商。apiKey 留空时保留已有密钥。"},
		func(_ context.Context, _ *mcp.CallToolRequest, in assistantSaveArgs) (*mcp.CallToolResult, any, error) {
			p, err := saveAssistantProvider(assistantConfigInput{
				ID: in.ID, Name: in.Name, BaseURL: in.BaseURL, Model: in.Model,
				APIKey: in.APIKey, Active: in.Active, Auto: in.Autopilot,
			})
			if err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpJSON(assistantConfigView{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model, KeyConfigured: true}), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_config_activate", Description: "把已有供应商设为当前供应商。"},
		func(_ context.Context, _ *mcp.CallToolRequest, in assistantActivateArgs) (*mcp.CallToolResult, any, error) {
			if err := activateAssistantProvider(in.ID); err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpText("已激活供应商 " + in.ID), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_config_delete", Description: "删除一个助手供应商。"},
		func(_ context.Context, _ *mcp.CallToolRequest, in assistantDeleteArgs) (*mcp.CallToolResult, any, error) {
			if err := deleteAssistantProvider(in.ID); err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpText("已删除供应商 " + in.ID), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_status", Description: "查看助手配置、会话数、记忆数和密码库条目数；不含秘密。"},
		func(context.Context, *mcp.CallToolRequest, assistantListArgs) (*mcp.CallToolResult, any, error) {
			disk, err := loadAssistantConfig()
			if err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpJSON(assistantConfigStatusOf(disk)), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "a2a_info", Description: "查看 A2A 入口、能力和鉴权方式。"},
		func(context.Context, *mcp.CallToolRequest, assistantListArgs) (*mcp.CallToolResult, any, error) {
			return mcpJSON(a.assistantA2ACard()), nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "assistant_run", Description: "调用本地文件助手。可用 folder 指定远程虚拟文件夹，用 files 指定要读取的文件；结果不含密钥。"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in assistantRunArgs) (*mcp.CallToolResult, any, error) {
			result, err := a.runAssistantFromMCP(ctx, in)
			if err != nil {
				return mcpErr(err), nil, nil
			}
			return mcpJSON(result), nil, nil
		})
}

type assistantRunView struct {
	Text   string    `json:"text"`
	Events []aiTurn  `json:"events,omitempty"`
	Choice *aiChoice `json:"choice,omitempty"`
	State  string    `json:"state"`
}

func (a *App) runAssistantFromMCP(ctx context.Context, in assistantRunArgs) (assistantRunView, error) {
	if strings.TrimSpace(in.Prompt) == "" && len(in.Files) == 0 {
		return assistantRunView{}, fmt.Errorf("prompt 和 files 至少提供一个")
	}
	if len(in.Files) > assistReadFiles {
		return assistantRunView{}, fmt.Errorf("一次最多指定 %d 个文件", assistReadFiles)
	}
	root, err := a.assistantRemoteRoot(in.Folder)
	if err != nil {
		return assistantRunView{}, err
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		return assistantRunView{}, err
	}
	prov, ok := activeAssistantProvider(disk)
	if !ok || prov.APIKey == "" {
		return assistantRunView{}, fmt.Errorf("请先配置并激活一个供应商")
	}
	text := strings.TrimSpace(in.Prompt)
	if text == "" {
		text = "请阅读这些文件"
	}
	var hist []*schema.Message
	for _, h := range in.History {
		switch strings.ToLower(strings.TrimSpace(h.Role)) {
		case "user":
			hist = append(hist, schema.UserMessage(h.Text))
		case "assistant":
			hist = append(hist, schema.AssistantMessage(h.Text, nil))
		}
	}
	hist = append(hist, schema.UserMessage(text))
	run := assistRun{
		origin: "mcp",
		prov:   prov, root: root, book: disk.ArchivePasswords, files: in.Files,
		autopilot: in.Autopilot || disk.Prefs.Autopilot, prefs: disk.Prefs, hist: hist,
		memories: readAssistantMemories(assistantSideRootFor(assistantConfigPath())),
	}
	if a.store != nil {
		run.hidden = a.store.remoteShowHidden()
		run.freqPaths = a.store.frequentPaths(20)
	}
	assistantRunMu.Lock()
	result, runErr := runAssistantHeadless(ctx, run)
	saved := sunvaleToolState.drainPending()
	sunvaleToolState.drainUndos()
	sunvaleToolState.drainNav()
	memAdd, memDrop := sunvaleToolState.drainMemory()
	assistantRunMu.Unlock()
	a.assistantApplyHeadlessSideEffects(saved, memAdd, memDrop)
	view := assistantRunView{Text: result.Text, Events: result.Events, Choice: result.Choice, State: "completed"}
	if result.Asked {
		view.State = "input-required"
	}
	if runErr != nil {
		view.State = "failed"
		return view, runErr
	}
	return view, nil
}

func mcpJSON(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpErr(err)
	}
	return mcpText(string(data))
}

// assistantHTTPState keeps the small amount of process state the HTTP
// layer needs outside the per-call MCP server. It exists so tests can reset
// tasks without rebuilding package-level variables.
func resetAssistantHTTPForTest() {
	assistantA2ATasks = &a2aTaskStore{tasks: map[string]*a2aTask{}}
	assistantConfigEpochMu.Lock()
	assistantConfigEpoch = 0
	assistantConfigEpochMu.Unlock()
	assistantConfigMu.Lock()
	assistantDiskMu.Lock()
	assistantConfigMu.Unlock()
	assistantDiskMu.Unlock()
}
