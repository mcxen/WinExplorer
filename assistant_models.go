package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	assistant "sunvale/internal/assistant"
)

// assistant_models.go is the composer's model chip. The list comes from the
// active provider's OpenAI-compatible /models endpoint and stays in memory;
// the chosen model is what assistant.json already stores on that provider.
// What models.dev adds: each row's real name, context and price, the
// capability badges, and the provider's own logo from the logo cache.

// assistantCatalogue holds the shared models.dev catalogue. One load for
// the whole process: the picker and the settings page read the same copy,
// and a refresh replaces the pointer atomically.
var (
	assistantCatalogueMu sync.RWMutex
	assistantCatalogue   *assistant.ModelCatalogue
	assistantCatLoading  bool
)

// ensureModelsDevCatalogue loads the catalogue once in the background. The
// first paint of the picker answers without it; when the load lands the
// window repaints and the rows gain their facts.
func ensureModelsDevCatalogue() {
	assistantCatalogueMu.RLock()
	loaded, loading := assistantCatalogue != nil, assistantCatLoading
	assistantCatalogueMu.RUnlock()
	if loaded || loading {
		return
	}
	assistantCatalogueMu.Lock()
	if assistantCatalogue != nil || assistantCatLoading {
		assistantCatalogueMu.Unlock()
		return
	}
	assistantCatLoading = true
	assistantCatalogueMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cat, err := assistant.LoadModelsDev(ctx)
		assistantCatalogueMu.Lock()
		assistantCatLoading = false
		if err == nil {
			assistantCatalogue = cat
		}
		assistantCatalogueMu.Unlock()
		// Publish for the headless decision helpers (vision, settings).
		if err == nil {
			assistant.SetSharedCatalogue(cat)
		}
		repaint()
	}()
}

// currentCatalogue is the loaded catalogue, or nil: every caller must work
// without it.
func currentCatalogue() *assistant.ModelCatalogue {
	assistantCatalogueMu.RLock()
	defer assistantCatalogueMu.RUnlock()
	return assistantCatalogue
}

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
		Options: a.assist.modelOptions(), Selected: selected, Width: 380,
	})
	if a.assist.modelPick.Open && !opened {
		a.assist.refreshConfig()
		if current, ok := a.assist.activeProvider(); ok {
			a.ensureProviderModels(current)
		}
		ensureModelsDevCatalogue()
	}
	if res.Chosen == "" {
		return
	}
	id, model, ok := strings.Cut(res.Chosen, "\x1f")
	if ok {
		a.assist.useModel(id, model)
	}
}

// modelOptions builds the picker rows: the provider's own logo leads, the
// catalogue's name and facts follow. A model the catalogue does not know
// still lists — the endpoint is the authority on what exists.
func (u *assistantUI) modelOptions() []chat.Option {
	choices := assistant.EnrichChoices(
		assistant.ModelChoices(u.providers, u.modelLists), currentCatalogue())
	out := make([]chat.Option, 0, len(choices))
	for _, choice := range choices {
		opt := chat.Option{
			ID: choice.Value, Label: choice.Label, Group: choice.Group,
		}
		if logo := providerLogo(choice.Provider); logo != nil {
			opt.Icon = logo.icon()
		}
		if choice.Info != nil {
			opt.Detail = assistant.ModelDetail(*choice.Info)
			opt.Description = assistant.ModelDescription(*choice.Info)
		}
		out = append(out, opt)
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
