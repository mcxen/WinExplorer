package assistant

import (
	"context"
	"encoding/json"
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

func TestPrefsSkillsAndJSON(t *testing.T) {
	prefs := Prefs{
		Autopilot: true, ExtraInstruction: "附加",
		DisabledSkills: []string{SkillMemory},
	}
	if !SkillDisabled(prefs, SkillMemory) || SkillDisabled(prefs, SkillFiles) {
		t.Fatalf("skill flags: %+v", prefs)
	}
	prefs.DisabledSkills = SetSkillDisabled(prefs.DisabledSkills, SkillFiles, true)
	prefs.DisabledSkills = SetSkillDisabled(prefs.DisabledSkills, SkillMemory, false)
	if SkillDisabled(prefs, SkillMemory) || !SkillDisabled(prefs, SkillFiles) {
		t.Fatalf("after toggle: %v", prefs.DisabledSkills)
	}
	data, err := json.Marshal(Config[struct{}]{Prefs: prefs})
	if err != nil {
		t.Fatal(err)
	}
	var round Config[struct{}]
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	if round.Prefs.ExtraInstruction != "附加" || !round.Prefs.Autopilot {
		t.Fatalf("round-trip prefs: %+v", round.Prefs)
	}
	if len(round.Prefs.DisabledSkills) != 1 || round.Prefs.DisabledSkills[0] != SkillFiles {
		t.Fatalf("round-trip skills: %v", round.Prefs.DisabledSkills)
	}
}

func TestVisionModeAndDetection(t *testing.T) {
	for _, tc := range []struct {
		model, vision string
		want          bool
	}{
		{"gpt-4o-mini", VisionAuto, true},
		{"qwen2.5-vl-72b-instruct", VisionAuto, true},
		{"claude-sonnet-4-5", VisionAuto, true},
		{"gemini-2.5-flash", VisionAuto, true},
		{"llama-3.2-11b-vision-instruct", VisionAuto, true},
		{"o3-mini", VisionAuto, true},
		{"grok-4-0709", VisionAuto, true},
		{"gemma-3-27b-it", VisionAuto, true},
		{"llama-4-scout-17b-16e-instruct", VisionAuto, true},
		{"gpt-3.5-turbo-instruct", VisionAuto, false},
		{"text-embedding-3-small", VisionAuto, false},
		{"qwen-max", VisionAuto, false},
		{"deepseek-chat", VisionAuto, false},
		{"llama-3.1-8b-instruct", VisionAuto, false},
		{"", VisionAuto, false},
		// An explicit choice beats the name.
		{"qwen-max", VisionOn, true},
		{"gpt-4o", VisionOff, false},
	} {
		p := Provider{Model: tc.model, Vision: tc.vision}
		if got := p.VisionEnabled(); got != tc.want {
			t.Fatalf("VisionEnabled(%q, %q) = %v, want %v", tc.model, tc.vision, got, tc.want)
		}
	}
}
