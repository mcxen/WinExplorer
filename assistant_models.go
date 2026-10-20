package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistant_models.go is the composer's model chip. The list comes from the
// active provider's OpenAI-compatible /models endpoint and stays in memory;
// the chosen model is what assistant.json already stores on that provider.

func (a *App) assistantModelPicker(c *ui.Context) {
	p, ok := a.assist.activeProvider()
	label := "选择模型"
	if ok && p.Model != "" {
		label = p.Model
	}
	if !ok {
		if chat.Chip(c, label, chat.ChipOptions{Icon: lucide.Bot, Caret: true}).Clicked() {
			a.assist.showProv = true
			a.assist.showHist, a.assist.showBook = false, false
		}
		return
	}
	opened := a.assist.modelPick.Open
	selected := p.ID + "\x1f" + p.Model
	res := chat.Picker(c, &a.assist.modelPick, chat.PickerOptions{
		Label: label, Icon: lucide.Bot, SearchLabel: "搜索模型", EmptyText: "没有匹配的模型",
		Options: a.assist.modelOptions(), Selected: selected, Width: 320,
	})
	if a.assist.modelPick.Open && !opened {
		a.assist.refreshConfig()
		if current, ok := a.assist.activeProvider(); ok {
			a.ensureProviderModels(current)
		}
	}
	if res.Chosen == "" {
		return
	}
	id, model, ok := strings.Cut(res.Chosen, "\x1f")
	if ok {
		a.assist.useModel(id, model)
	}
}

func (u *assistantUI) modelOptions() []chat.Option {
	var out []chat.Option
	for _, p := range u.providers {
		models := u.modelLists[p.ID]
		if len(models) == 0 && p.Model != "" {
			models = []string{p.Model}
		}
		seen := map[string]bool{}
		if p.Model != "" && !containsStr(models, p.Model) {
			models = append([]string{p.Model}, models...)
		}
		for _, model := range models {
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, chat.Option{
				ID: p.ID + "\x1f" + model, Label: model, Group: p.Name,
			})
		}
	}
	return out
}

// useModel points the assistant at one configured provider and model.
// The key stays where it was; an unknown provider is ignored.
func (u *assistantUI) useModel(id, model string) {
	model = strings.TrimSpace(model)
	if id == "" || model == "" {
		return
	}
	if u.diskPath != "" {
		u.refreshConfig()
	}
	found := false
	for i := range u.providers {
		if u.providers[i].ID != id {
			continue
		}
		if u.activeID == id && u.providers[i].Model == model {
			return
		}
		u.providers[i].Model = model
		found = true
		break
	}
	if !found {
		return
	}
	u.activeID = id
	u.save()
}

func (a *App) ensureProviderModels(p aiProvider) {
	if p.ID == "" || p.APIKey == "" || p.BaseURL == "" {
		return
	}
	if a.assist.modelLists[p.ID] != nil || a.assist.modelBusy[p.ID] {
		return
	}
	if a.assist.modelLists == nil {
		a.assist.modelLists = map[string][]string{}
	}
	if a.assist.modelBusy == nil {
		a.assist.modelBusy = map[string]bool{}
	}
	a.assist.modelBusy[p.ID] = true
	go func(prov aiProvider) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		models, err := fetchProviderModels(ctx, prov)
		a.postAssist(func() {
			delete(a.assist.modelBusy, prov.ID)
			if err != nil {
				a.assist.modelLists[prov.ID] = []string{}
				if a.assist.modelPick.Open {
					a.statusMsg = "读不到模型列表，仍可使用已保存的模型"
				}
				return
			}
			a.assist.modelLists[prov.ID] = models
		})
	}(p)
}

func fetchProviderModels(ctx context.Context, p aiProvider) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型列表 %d", res.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Data) == 0 {
		return nil, fmt.Errorf("模型列表没有内容")
	}
	seen := map[string]bool{}
	var models []string
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
		if len(models) >= 100 {
			break
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表没有内容")
	}
	return models, nil
}
