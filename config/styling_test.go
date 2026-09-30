package config

import (
	"strings"
	"testing"
)

func TestDefaultsStylingMatchesSchema(t *testing.T) {
	s := Defaults().Styling
	if s.Scale != 1.01 || s.Rounding != RoundingSm || s.Appearance != AppearanceAuto || s.PaletteBaseTheme != "" {
		t.Errorf("styling defaults = %+v", s)
	}
	if got := s.ActivePalette(); got != wayleTheme {
		t.Errorf("default palette = %+v, want the wayle theme", got)
	}
}

func TestLoadFileAppliesStylingSection(t *testing.T) {
	path := writeConfig(t, `
[styling]
scale = 1.5
rounding = "lg"
appearance = "dark"
palette-base-theme = "nord"
theme-provider = "matugen"

[styling.palette]
bg = "#000000"
fg-muted = "#abc"
blue = "#11223344"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := StylingConfig{
		Scale: 1.5, Rounding: RoundingLg, Appearance: AppearanceDark, PaletteBaseTheme: "nord",
		Palette: paletteConfigOf(wayleTheme),
	}
	want.Palette.Bg, want.Palette.FgMuted, want.Palette.Blue = mustHex("#000000"), mustHex("#abc"), mustHex("#11223344")
	if cfg.Styling != want {
		t.Errorf("styling =\n%+v\nwant\n%+v", cfg.Styling, want)
	}
	// The same table feeds the color extractor half.
	if cfg.ColorExtractor.ThemeProvider != ThemeMatugen {
		t.Errorf("theme-provider = %q, want matugen", cfg.ColorExtractor.ThemeProvider)
	}
}

func TestStylingScaleClampsLikeScaleFactor(t *testing.T) {
	for in, want := range map[string]float64{"0.1": 0.25, "-2": 0.25, "3.5": 3.0, "2": 2} {
		cfg, err := LoadFile(writeConfig(t, "[styling]\nscale = "+in+"\n"))
		if err != nil {
			t.Fatalf("scale %s: %v", in, err)
		}
		if cfg.Styling.Scale != want {
			t.Errorf("scale %s = %v, want %v", in, cfg.Styling.Scale, want)
		}
	}
}

func TestLoadFileRejectsBadStyling(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"[styling]\nrounding = \"xl\"", "rounding"},
		{"[styling]\nappearance = \"sepia\"", "appearance"},
		{"[styling]\nscale = \"big\"", "scale"},
		{"[styling]\npalette-base-theme = 3", "palette-base-theme"},
		{"[styling.palette]\nbg = \"141420\"", "hex"},
		{"[styling.palette]\nprimary = \"#12345\"", "hex"},
		{"[styling.palette]\nred = \"red\"", "hex"},
	} {
		cfg, err := LoadFile(writeConfig(t, tc.body+"\n"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want an error naming %q", tc.body, err, tc.want)
			continue
		}
		if cfg.Styling != DefaultsStyling() {
			t.Errorf("%q: a failed [styling] must leave the defaults, got %+v", tc.body, cfg.Styling)
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
