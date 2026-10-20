package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	assistant "sunvale/internal/assistant"
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
	choices := assistant.ModelChoices(u.providers, u.modelLists)
	out := make([]chat.Option, 0, len(choices))
	for _, choice := range choices {
		out = append(out, chat.Option{
			ID: choice.Value, Label: choice.Label, Group: choice.Group,
		})
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
	return assistant.FetchProviderModels(ctx, p)
}
