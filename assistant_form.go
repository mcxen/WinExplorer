package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/forms"
)

// assistant_form.go is present_form: the model hands a declarative form
// schema, the tool returns an OpenUI program (and a ```form JSON copy) so
// the pane paints the same controls Settings uses. Submit still only posts
// the next user message.

type presentFormArgs struct {
	Title       string `json:"title,omitempty" jsonschema:"表单标题"`
	Description string `json:"description,omitempty" jsonschema:"一句说明"`
	ID          string `json:"id,omitempty" jsonschema:"表单 id，可空"`
	FieldsJSON  string `json:"fields_json,omitempty" jsonschema:"fields 数组的 JSON 字符串，每项含 id/kind/label，可选 placeholder/required/options/path_mode/value"`
	SubmitLabel string `json:"submit_label,omitempty" jsonschema:"提交按钮文字，默认「提交」"`
	SubmitMsg   string `json:"submit_message,omitempty" jsonschema:"提交后发给助手的一句中文，默认与按钮文字相同"`
	Preset      string `json:"preset,omitempty" jsonschema:"可选预设：schedule|rename|share|search|classify|backup|filelist|image|video，忽略 fields_json"`
	PrefillJSON string `json:"prefill_json,omitempty" jsonschema:"预设预填值的 JSON 对象，如 {\"time\":\"19:00\"}"`
}

func (s *sunvaleTools) presentForm(_ context.Context, _ *mcp.CallToolRequest, args presentFormArgs) (*mcp.CallToolResult, any, error) {
	var f forms.Form
	prefill := map[string]string{}
	if strings.TrimSpace(args.PrefillJSON) != "" {
		_ = json.Unmarshal([]byte(args.PrefillJSON), &prefill)
	}
	preset := strings.TrimSpace(args.Preset)
	if preset != "" {
		scene, ok := forms.Scene(preset, prefill)
		if !ok {
			return mcpErr(fmt.Errorf("未知 preset %q（可用：%s）", preset, strings.Join(forms.ScenePresetIDs(), ", "))), nil, nil
		}
		f = scene
		if t := strings.TrimSpace(args.Title); t != "" {
			f.Title = t
		}
		if d := strings.TrimSpace(args.Description); d != "" {
			f.Description = d
		}
	} else {
		raw := strings.TrimSpace(args.FieldsJSON)
		if raw == "" {
			return mcpErr(fmt.Errorf("fields_json 不能为空（或改用 preset=%s）", strings.Join(forms.ScenePresetIDs(), "|"))), nil, nil
		}
		// Accept either a bare array or a full Form object.
		if strings.HasPrefix(raw, "[") {
			raw = `{"fields":` + raw + `}`
		}
		parsed, err := forms.ParseJSON(raw)
		if err != nil {
			return mcpErr(err), nil, nil
		}
		f = *parsed
		if t := strings.TrimSpace(args.Title); t != "" {
			f.Title = t
		}
		if d := strings.TrimSpace(args.Description); d != "" {
			f.Description = d
		}
		if id := strings.TrimSpace(args.ID); id != "" {
			f.ID = id
		}
	}
	label := strings.TrimSpace(args.SubmitLabel)
	if label == "" {
		label = "提交"
	}
	msg := strings.TrimSpace(args.SubmitMsg)
	if msg == "" {
		msg = label
	}
	if len(f.Actions) == 0 {
		f.Actions = []forms.Action{{ID: "submit", Label: label, Variant: "primary", Message: msg}}
	} else if strings.TrimSpace(args.SubmitLabel) != "" || strings.TrimSpace(args.SubmitMsg) != "" {
		f.Actions[0].Label = label
		f.Actions[0].Message = msg
		f.Actions[0].Variant = "primary"
	}
	f.Normalize()
	if len(f.Fields) == 0 {
		return mcpErr(fmt.Errorf("表单没有填写项")), nil, nil
	}
	var b strings.Builder
	b.WriteString("请用户填写后提交。调用后停止本轮，等用户点提交。\n")
	b.WriteString(forms.FenceOpenUI(f.ToOpenUI()))
	return mcpText(b.String()), nil, nil
}
