package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadFileAppliesStylingSection(t *testing.T) {
	path := writeConfig(t, `
[styling]
scale = 1.5
rounding = "lg"
appearance = "dark"
palette_base_theme = "nord"
theme-provider = "matugen"
matugen-contrast = 3

[styling.palette]
bg = "#000000"
fg-muted = "#abc"
blue = "#11223344"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := DefaultsStyling()
	want.Scale, want.Rounding, want.Appearance, want.PaletteBaseTheme = 1.5, RoundingLg, AppearanceDark, "nord"
	want.ColorExtractor.ThemeProvider = ThemeProviderMatugen
	want.ColorExtractor.MatugenContrast = 1 // clamped, with a warning
	want.Palette.Bg, want.Palette.FgMuted, want.Palette.Blue = mustHex("#000000"), mustHex("#abc"), mustHex("#11223344")
	if !reflect.DeepEqual(cfg.Styling, want) {
		t.Errorf("styling =\n%+v\nwant\n%+v", cfg.Styling, want)
	}
}

// Each bad [styling] value is a diagnostic for its own key; that key
// keeps its default.
func TestStylingBadValuesAreFieldDiagnostics(t *testing.T) {
	for _, tc := range []struct{ body, path string }{
		{"[styling]\nrounding = \"xl\"", "styling.rounding"},
		{"[styling]\nappearance = \"sepia\"", "styling.appearance"},
		{"[styling]\nscale = \"big\"", "styling.scale"},
		{"[styling]\ntheme-provider = \"base16\"", "styling.theme-provider"},
		{"[styling.palette]\nbg = \"141420\"", "styling.palette.bg"},
		{"[styling.palette]\nprimary = \"#12345\"", "styling.palette.primary"},
		{"[styling.palette]\nred = \"red\"", "styling.palette.red"},
	} {
		cfg, err := LoadFile(writeConfig(t, tc.body+"\n"))
		if err == nil || !strings.Contains(err.Error(), tc.path) {
			t.Errorf("%q: diagnostics %v, want one for %s", tc.body, err, tc.path)
			continue
		}
		if !reflect.DeepEqual(cfg.Styling, DefaultsStyling()) {
			t.Errorf("%q: the bad key did not keep its default: %+v", tc.body, cfg.Styling)
		}
	}
}

func TestAppearanceForcedLight(t *testing.T) {
	for _, tc := range []struct {
		a             Appearance
		light, forced bool
	}{
		{AppearanceAuto, false, false},
		{AppearanceLight, true, true},
		{AppearanceDark, false, true},
	} {
		light, forced := tc.a.ForcedLight()
		if light != tc.light || forced != tc.forced {
			t.Errorf("%s: ForcedLight = (%v, %v), want (%v, %v)", tc.a, light, forced, tc.light, tc.forced)
		}
	}
}

func TestActivePaletteSwapsToTheForcedVariant(t *testing.T) {
	mocha, _ := PaletteByName("catppuccin-mocha")
	latte, _ := PaletteByName("catppuccin-latte")
	configured := DefaultsStyling()
	configured.Palette.Bg = mustHex("#010203")
	base := configured.ActivePalette()
	if base.Bg != "#010203" {
		t.Fatalf("auto palette bg = %q, want the configured color", base.Bg)
	}
	for _, tc := range []struct {
		name       string
		appearance Appearance
		baseTheme  string
		want       Palette
	}{
		{"light forces the light sibling", AppearanceLight, "catppuccin-mocha", latte},
		{"light maps a dark sub-variant", AppearanceLight, "catppuccin-frappe", latte},
		{"dark forces the dark sibling", AppearanceDark, "catppuccin-latte", mocha},
		{"auto keeps the configured colors", AppearanceAuto, "catppuccin-mocha", base},
		{"already light keeps the configured colors", AppearanceLight, "catppuccin-latte", base},
		{"already dark keeps the configured colors", AppearanceDark, "catppuccin-frappe", base},
		{"a theme without a pair keeps them", AppearanceLight, "dracula", base},
		{"an unknown theme keeps them", AppearanceDark, "my-theme", base},
		{"no base theme keeps them", AppearanceLight, "", base},
	} {
		s := configured
		s.Appearance, s.PaletteBaseTheme = tc.appearance, tc.baseTheme
		if got := s.ActivePalette(); got != tc.want {
			t.Errorf("%s: palette = %+v, want %+v", tc.name, got, tc.want)
		}
		if s.PaletteBaseTheme != tc.baseTheme {
			t.Errorf("%s: ActivePalette changed the base theme", tc.name)
		}
	}
}
