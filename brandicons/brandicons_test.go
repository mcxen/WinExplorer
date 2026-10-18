package brandicons

import "testing"

// TestLookupMatchesMakers checks the model-string matching: the real model
// names each maker ships, case differences, and a maker this package does
// not carry.
func TestLookupMatchesMakers(t *testing.T) {
	cases := []struct{ model, want string }{
		{"APPLE SSD AP0256Z", "Apple"},
		{"apple ssd ap0512m", "Apple"},
		{"ST4000DM004-2CV104", "Seagate"},
		{"WDC WD40EFRX-68N32N0", "Western Digital"},
		{"WD_BLACK SN850X", "Western Digital"},
		{"SanDisk Extreme SSD", "SanDisk"},
		{"SAMSUNG MZ-V7S1T0", "Samsung"},
		{"KINGSTON SA400S37480G", "Kingston"},
		{"TOSHIBA KXG60ZNV1T02", "Toshiba"},
		{"HGST HDS724040ALE640", "Hitachi"},
		{"INTEL SSDPEKKW512G7", "Intel"},
		{"DELL GM100SATA", "Dell"},
		{"HP SSD S700", "HP"},
		{"SYNOLOGY HAT5300", "Synology"},
		{"QNAP SSD-1T", "QNAP"},
		{"Lenovo ThinkPad SSD", "Lenovo"},
	}
	for _, c := range cases {
		logo := Lookup(c.model)
		if logo == nil {
			t.Fatalf("Lookup(%q) = nil, want %s", c.model, c.want)
		}
		if logo.Label != c.want {
			t.Fatalf("Lookup(%q).Label = %q, want %q", c.model, logo.Label, c.want)
		}
		if logo.SVG == nil {
			t.Fatalf("Lookup(%q) has no parsed SVG", c.model)
		}
	}
}

// TestLookupUnknown checks that a maker outside the set — and an empty
// model — yields no logo, so the page falls back to the generic glyph.
func TestLookupUnknown(t *testing.T) {
	for _, model := range []string{"", "   ", "CRUCIAL CT1000MX500SSD1", "KIOXIA KXG80ZNV1T02"} {
		if logo := Lookup(model); logo != nil {
			t.Fatalf("Lookup(%q) = %+v, want nil", model, logo)
		}
	}
}

// TestEveryMakerHasALogo checks the icon files are embedded for every
// brand in the table: a brand whose file went missing would silently show
// the wrong thing.
func TestEveryMakerHasALogo(t *testing.T) {
	for _, b := range brands {
		if load(b.name) == nil {
			t.Fatalf("brand %q has no embedded logo", b.name)
		}
	}
}

// TestPrefixesAreUpper checks the table's contract: matching upper-cases
// the model, so a lower-case prefix would never match.
func TestPrefixesAreUpper(t *testing.T) {
	for _, b := range brands {
		for _, p := range b.prefixes {
			for _, r := range p {
				if r >= 'a' && r <= 'z' {
					t.Fatalf("%s: prefix %q is not upper case", b.name, p)
				}
			}
		}
	}
}
