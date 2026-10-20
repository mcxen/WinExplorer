package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProviderModelData(t *testing.T) {
	base := []Provider{{ID: "old", Name: "旧", Model: "m1"}}
	local := []Provider{{ID: "old", Name: "旧", Model: "m1"}}
	disk := []Provider{{ID: "old", Name: "旧", Model: "fresh"}, {ID: "new", Name: "新", Model: "m2"}}
	got := MergeProviders(base, local, disk)
	if len(got) != 2 || got[0].Model != "fresh" || got[1].ID != "new" {
		t.Fatalf("merged providers: %+v", got)
	}
	active, ok := ActiveProvider(got, "new")
	if !ok || active.Model != "m2" {
		t.Fatalf("active provider: %+v %v", active, ok)
	}

	choices := ModelChoices(got, map[string][]string{
		"old": {"fresh", "m0", "fresh"},
		"new": nil,
	})
	var values []string
	for _, choice := range choices {
		values = append(values, choice.Value)
	}
	want := []string{"old\x1ffresh", "old\x1fm0", "new\x1fm2"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("model choices: %q, want %q", values, want)
	}
}

func TestFetchProviderModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer key" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m2"},{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer srv.Close()
	got, err := FetchProviderModels(context.Background(), Provider{BaseURL: srv.URL + "/v1", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"m1", "m2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models %v, want %v", got, want)
	}
}
