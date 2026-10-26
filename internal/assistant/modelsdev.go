package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// modelsdev.go is the client for models.dev, the community catalogue of AI
// models. Two things come from it: what a model can do (vision, reasoning,
// tools, context size) and what a provider is called and reached at. The
// catalogue replaces name guessing — the request-time text retry stays as
// the safety net, but the first ask is informed.
//
// The catalogue is data, not a service the app depends on at run time: a
// seven-day cache in the user cache dir keeps every start offline-cheap,
// and a machine that never sees the network still works exactly as before.

var modelsDevURL = "https://models.dev/api.json"

const (
	// modelsDevTTL is how long a cached catalogue answers without a
	// re-fetch. Model lists move slowly; a week-old fact is still a fact.
	modelsDevTTL = 7 * 24 * time.Hour
	// modelsDevFetchTimeout bounds one refresh.
	modelsDevFetchTimeout = 20 * time.Second
	// modelsDevMaxBytes caps the body: the full catalogue is ~2 MB, and a
	// hostile mirror should not flood memory.
	modelsDevMaxBytes = 8 << 20
)

// ModelInfo is one model as models.dev describes it. Only the fields the
// assistant shows or decides with are kept; the rest stay on the wire.
type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Family      string   `json:"family,omitempty"`
	Provider    string   `json:"-"` // provider id from the catalogue key
	Attachment  bool     `json:"attachment,omitempty"`
	Reasoning   bool     `json:"reasoning,omitempty"`
	ToolCall    bool     `json:"tool_call,omitempty"`
	Input       []string `json:"-"` // modalities.input, e.g. text/image/audio
	Output      []string `json:"-"`
	Context     int      `json:"-"`
	OutputLimit int      `json:"-"`
	Knowledge   string   `json:"knowledge,omitempty"`
	ReleaseDate string   `json:"release_date,omitempty"`
	CostIn      float64  `json:"-"`
	CostOut     float64  `json:"-"`
}

// SeesImages reports whether the model accepts pictures, from the
// catalogue's own modality list rather than a name heuristic.
func (m ModelInfo) SeesImages() bool {
	for _, mod := range m.Input {
		if mod == "image" {
			return true
		}
	}
	return false
}

// ModelCatalogue is the parsed api.json: provider id → models.
type ModelCatalogue struct {
	Providers map[string]modelsDevProvider `json:"providers"`
	FetchedAt time.Time                    `json:"fetchedAt"`
}

type modelsDevProvider struct {
	ID     string                    `json:"-"` // the catalogue key, filled in when listed
	Name   string                    `json:"name"`
	API    string                    `json:"api,omitempty"`
	Env    []string                  `json:"env,omitempty"`
	Doc    string                    `json:"doc,omitempty"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Family      string `json:"family,omitempty"`
	Attachment  bool   `json:"attachment,omitempty"`
	Reasoning   bool   `json:"reasoning,omitempty"`
	ToolCall    bool   `json:"tool_call,omitempty"`
	Knowledge   string `json:"knowledge,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
	Modalities  struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input     float64 `json:"input"`
		Output    float64 `json:"output"`
		CacheRead float64 `json:"cache_read"`
	} `json:"cost"`
}

// modelsDevCachePathForTest lets a test point the cache at its own world.
var modelsDevCachePathForTest string

// ModelsDevCachePath is where the parsed catalogue lives. Under the user
// cache dir so an uninstall leaves nothing behind and a corrupt file is
// freely deletable.
func ModelsDevCachePath() (string, error) {
	if modelsDevCachePathForTest != "" {
		return modelsDevCachePathForTest, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sunvale", "modelsdev.json"), nil
}

// modelsDevMu serialises refreshes: two windows opening the picker must not
// both fetch the catalogue.
var modelsDevMu sync.Mutex

// sharedCatalogue is the process-wide loaded catalogue, set by the
// application once the first picker or settings page needs it. Kept here
// rather than in the window package so the decision helpers (vision, the
// settings hints) can read it headlessly too.
var (
	sharedCatalogueMu sync.RWMutex
	sharedCatalogue   *ModelCatalogue
)

// SetSharedCatalogue publishes the loaded catalogue to the whole process.
func SetSharedCatalogue(cat *ModelCatalogue) {
	sharedCatalogueMu.Lock()
	sharedCatalogue = cat
	sharedCatalogueMu.Unlock()
}

// CurrentCatalogue is the published catalogue, or nil. Decision code must
// work without it: offline machines still run the assistant.
func CurrentCatalogue() *ModelCatalogue {
	sharedCatalogueMu.RLock()
	defer sharedCatalogueMu.RUnlock()
	return sharedCatalogue
}

// LoadModelsDev returns the catalogue, fresh from the cache or after a
// refresh. A failed refresh with a stale cache still answers — old facts
// beat no facts. A machine with neither gets a nil catalogue, which every
// caller treats as "no data, fall back".
func LoadModelsDev(ctx context.Context) (*ModelCatalogue, error) {
	modelsDevMu.Lock()
	defer modelsDevMu.Unlock()
	path, err := ModelsDevCachePath()
	if err != nil {
		return nil, err
	}
	var cat ModelCatalogue
	absent, err := ReadJSON(path, &cat)
	if err != nil {
		// A torn or unreadable cache is an absent one: refresh from the
		// wire instead of answering with empty providers forever.
		absent = true
	}
	if !absent && cat.Providers != nil && time.Since(cat.FetchedAt) < modelsDevTTL {
		return &cat, nil
	}
	fresh, err := fetchModelsDev(ctx)
	if err != nil {
		if absent {
			return nil, err
		}
		return &cat, nil
	}
	if err := WriteJSON(path, fresh); err != nil {
		return fresh, nil // a cache write failure is not a read failure
	}
	return fresh, nil
}

func fetchModelsDev(ctx context.Context) (*ModelCatalogue, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsDevFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev 目录请求 %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, modelsDevMaxBytes))
	if err != nil {
		return nil, err
	}
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, err
	}
	return &ModelCatalogue{Providers: providers, FetchedAt: time.Now()}, nil
}

// WriteJSON writes durable state without the 0600 contract of WritePrivate:
// the catalogue holds no secrets.
func WriteJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LookupModel finds one model by id across providers. Empty when unknown:
// callers fall back to the name heuristic, never fail the turn.
func (c *ModelCatalogue) LookupModel(model string) (ModelInfo, bool) {
	if c == nil || model == "" {
		return ModelInfo{}, false
	}
	want := strings.ToLower(strings.TrimSpace(model))
	// Exact id first: "gpt-4o-2024-11-20" must not match "gpt-4o".
	for pid, prov := range c.Providers {
		if m, ok := prov.Models[want]; ok {
			return m.info(pid), true
		}
	}
	// Catalogue ids and route names drift; accept an exact name match and
	// a prefix the other way ("gpt-4o" route → "gpt-4o-2024-11-20" entry),
	// newest release first when several match.
	var hits []ModelInfo
	for pid, prov := range c.Providers {
		for id, m := range prov.Models {
			idLow := strings.ToLower(id)
			nameLow := strings.ToLower(m.Name)
			if idLow == want || nameLow == want || strings.HasPrefix(idLow+"/", want+"/") ||
				(len(want) > len(idLow) && strings.HasPrefix(want+"-", idLow+"-")) {
				hits = append(hits, m.info(pid))
			}
		}
	}
	if len(hits) == 0 {
		return ModelInfo{}, false
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ReleaseDate > hits[j].ReleaseDate })
	return hits[0], true
}

// LookupProvider returns one provider's display name and OpenAI-compatible
// endpoint, when the catalogue knows both. The endpoint is a suggestion for
// the add-provider form, never silently applied.
func (c *ModelCatalogue) LookupProvider(id string) (name, api string, ok bool) {
	if c == nil {
		return "", "", false
	}
	p, found := c.Providers[id]
	if !found {
		return "", "", false
	}
	return p.Name, p.API, true
}

// OpenAICompatibleProviders lists catalogue providers that publish an
// OpenAI-style endpoint, in name order: the candidates the "add provider"
// flow offers, because sunvale talks to exactly that shape.
func (c *ModelCatalogue) OpenAICompatibleProviders() []modelsDevProvider {
	if c == nil {
		return nil
	}
	var out []modelsDevProvider
	for id, p := range c.Providers {
		if p.API == "" {
			continue
		}
		p.ID = id
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Models) != len(out[j].Models) {
			return len(out[i].Models) > len(out[j].Models)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m modelsDevModel) info(provider string) ModelInfo {
	return ModelInfo{
		ID: m.ID, Name: m.Name, Description: m.Description, Family: m.Family,
		Provider: provider, Attachment: m.Attachment, Reasoning: m.Reasoning,
		ToolCall: m.ToolCall, Input: m.Modalities.Input, Output: m.Modalities.Output,
		Context: m.Limit.Context, OutputLimit: m.Limit.Output,
		Knowledge: m.Knowledge, ReleaseDate: m.ReleaseDate,
		CostIn: m.Cost.Input, CostOut: m.Cost.Output,
	}
}
