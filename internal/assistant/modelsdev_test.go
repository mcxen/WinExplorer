package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCatalogue(t *testing.T) *ModelCatalogue {
	t.Helper()
	data, err := os.ReadFile("testdata/modelsdev.json")
	if err != nil {
		t.Fatal(err)
	}
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		t.Fatal(err)
	}
	return &ModelCatalogue{Providers: providers, FetchedAt: time.Now()}
}

func TestModelsDevLookupModel(t *testing.T) {
	cat := testCatalogue(t)

	// Exact id: vision comes from the catalogue's modality list, not a name.
	m, ok := cat.LookupModel("gpt-4o")
	if !ok {
		t.Fatal("gpt-4o not found")
	}
	if !m.SeesImages() || !m.ToolCall || m.Context <= 0 {
		t.Fatalf("gpt-4o info: %+v", m)
	}
	if m.Name != "GPT-4o" || m.Provider != "openai" {
		t.Fatalf("gpt-4o identity: %q %q", m.Name, m.Provider)
	}

	// A text-only model must not look like it sees images.
	m, ok = cat.LookupModel("text-embedding-3-small")
	if !ok || m.SeesImages() {
		t.Fatalf("embedding model: ok=%v %+v", ok, m)
	}

	// Route name → dated entry: "gpt-4o-2024-11-20" style suffixes resolve
	// to the newest matching release.
	m, ok = cat.LookupModel("gemini-2.5-pro")
	if !ok || !m.SeesImages() {
		t.Fatalf("gemini-2.5-pro: ok=%v %+v", ok, m)
	}

	// Unknown ids answer nothing, so callers fall back gracefully.
	if _, ok := cat.LookupModel("totally-made-up"); ok {
		t.Fatal("unknown model found")
	}
	if _, ok := cat.LookupModel(""); ok {
		t.Fatal("empty model found")
	}
	var nilCat *ModelCatalogue
	if _, ok := nilCat.LookupModel("gpt-4o"); ok {
		t.Fatal("nil catalogue found a model")
	}
}

func TestModelsDevProviderAndList(t *testing.T) {
	cat := testCatalogue(t)

	name, api, ok := cat.LookupProvider("zhipuai")
	if !ok || name != "Zhipu AI" || !strings.HasPrefix(api, "https://open.bigmodel.cn") {
		t.Fatalf("zhipuai: %q %q %v", name, api, ok)
	}
	if _, _, ok := cat.LookupProvider("no-such"); ok {
		t.Fatal("unknown provider found")
	}

	list := cat.OpenAICompatibleProviders()
	if len(list) == 0 {
		t.Fatal("no openai-compatible providers")
	}
	for _, p := range list {
		if p.API == "" || p.ID == "" || p.Name == "" {
			t.Fatalf("incomplete provider: %+v", p)
		}
	}
}

func TestModelsDevCacheLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"openai":{"name":"OpenAI","models":{"gpt-4o":{"id":"gpt-4o","name":"GPT-4o","modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":128000}}}}}`))
	}))
	defer srv.Close()

	// Point the URL and the cache path at the test's own world.
	oldURL, oldPath := modelsDevURL, modelsDevCachePathForTest
	modelsDevURL = srv.URL
	path := filepath.Join(t.TempDir(), "cat.json")
	modelsDevCachePathForTest = path
	t.Cleanup(func() { modelsDevURL, modelsDevCachePathForTest = oldURL, oldPath })

	cat, err := LoadModelsDev(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.LookupModel("gpt-4o"); !ok {
		t.Fatal("fresh catalogue lost the model")
	}

	// A stale cache is better than no answer: a dead server still serves.
	stale := &ModelCatalogue{
		FetchedAt: time.Now().Add(-30 * 24 * time.Hour),
		Providers: map[string]modelsDevProvider{
			"openai": {Name: "OpenAI", Models: map[string]modelsDevModel{
				"gpt-4o": {ID: "gpt-4o", Name: "GPT-4o"},
			}},
		},
	}
	if err := WriteJSON(path, stale); err != nil {
		t.Fatal(err)
	}
	modelsDevURL = "http://127.0.0.1:1/nope"
	cat, err = LoadModelsDev(context.Background())
	if err != nil {
		t.Fatalf("stale cache not served: %v", err)
	}
	if _, ok := cat.LookupModel("gpt-4o"); !ok {
		t.Fatal("stale catalogue lost the model")
	}

	// Neither cache nor network: nil, and the caller falls back.
	modelsDevCachePathForTest = filepath.Join(t.TempDir(), "absent.json")
	modelsDevURL = "http://127.0.0.1:1/nope"
	if cat, err := LoadModelsDev(context.Background()); err == nil || cat != nil {
		t.Fatalf("expected an error with no cache and no network, got %v %v", cat, err)
	}
}
