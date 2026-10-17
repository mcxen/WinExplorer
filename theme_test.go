package main

import "testing"

// TestAppearanceSettingRoundTrip checks the stored 外观 values are the three
// sources and nothing else, so an unknown string falls back to 跟随系统
// rather than to a stale appearance.
func TestAppearanceSettingRoundTrip(t *testing.T) {
	for _, c := range []struct {
		setting string
		source  string
	}{
		{appearanceLight, "light"},
		{appearanceDark, "dark"},
		{"", "system"},
	} {
		got := appearanceSource(c.setting)
		if string(got) != c.source {
			t.Errorf("appearanceSource(%q) = %q, want %q", c.setting, got, c.source)
		}
		if back := appearanceSetting(got); back != c.setting {
			t.Errorf("appearanceSetting(%q) = %q, want %q", got, back, c.setting)
		}
	}
	if got := appearanceSource("solarized"); got != "system" {
		t.Errorf("appearanceSource(unknown) = %q, want system", got)
	}
}

// TestAppearanceSegments checks the settings page's segmented control and
// the stored setting name the same three appearances, and that a fresh
// state starts on 跟随系统.
func TestAppearanceSegments(t *testing.T) {
	for setting, want := range map[string]int{
		"":              0, // 跟随系统
		appearanceLight: 1,
		appearanceDark:  2,
		"twilight":      0, // unknown settings read as 跟随系统
	} {
		if got := appearanceIndex(setting); got != want {
			t.Errorf("appearanceIndex(%q) = %d, want %d", setting, got, want)
		}
	}
	if got := defaultSettings().Appearance; got != "" {
		t.Errorf("fresh settings appearance = %q, want the system's", got)
	}
}
