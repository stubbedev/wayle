package config

import (
	"testing"
)

func TestParseColorValueVariants(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want ColorValue
	}{
		{"bg-surface", ColorValue{Kind: ColorToken, Token: TokenBgSurface}},
		{"border-accent", ColorValue{Kind: ColorToken, Token: TokenBorderAccent}},
		{"transparent", ColorValue{Kind: ColorTransparent}},
		{"auto", ColorValue{Kind: ColorAuto}},
		{"#414868", ColorValue{Kind: ColorCustom, Hex: "#414868"}},
		{"#abc", ColorValue{Kind: ColorCustom, Hex: "#abc"}},
		{"#41486880", ColorValue{Kind: ColorCustom, Hex: "#41486880"}},
	} {
		got, err := ParseColorValue(tc.in)
		if err != nil {
			t.Errorf("ParseColorValue(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseColorValue(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseColorValueRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		"not-a-token", "bg-surface-x", "", "#12", "#12345", "#gggggg", "0x414868",
	} {
		if _, err := ParseColorValue(in); err == nil {
			t.Errorf("ParseColorValue(%q): want an error, got nil", in)
		}
	}
}

func TestLoadFileAppliesColorsAndBorders(t *testing.T) {
	path := writeConfig(t, `
[bar]
bg = "#ff00ff"
background-opacity = 80
border-color = "border-strong"
border-location = "top"
border-width = 3
inset-edge = "4px"
button-group-background = "bg-overlay"
button-group-opacity = 60
button-group-border-location = "all"
button-group-rounding = "lg"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Bar.BG.Kind != ColorCustom || cfg.Bar.BG.Hex != "#ff00ff" {
		t.Errorf("bg = %+v, want the custom hex", cfg.Bar.BG)
	}
	if cfg.Bar.BackgroundOpacity != 80 {
		t.Errorf("background-opacity = %d, want 80", cfg.Bar.BackgroundOpacity)
	}
	if cfg.Bar.BorderColor.Token != TokenBorderStrong {
		t.Errorf("border-color = %+v, want the border-strong token", cfg.Bar.BorderColor)
	}
	if cfg.Bar.BorderLocation != BorderTop || cfg.Bar.BorderWidth != 3 {
		t.Errorf("border = %q/%d, want top/3", cfg.Bar.BorderLocation, cfg.Bar.BorderWidth)
	}
	if cfg.Bar.InsetEdge.Unit != SizePixels || cfg.Bar.InsetEdge.Value != 4 {
		t.Errorf("inset-edge = %+v, want 4px", cfg.Bar.InsetEdge)
	}
	if cfg.Bar.ButtonGroupBackground.Token != TokenBgOverlay {
		t.Errorf("button-group-background = %+v", cfg.Bar.ButtonGroupBackground)
	}
	if cfg.Bar.ButtonGroupOpacity != 60 {
		t.Errorf("button-group-opacity = %d, want 60", cfg.Bar.ButtonGroupOpacity)
	}
	if cfg.Bar.ButtonGroupBorderLocation != BorderAll {
		t.Errorf("button-group-border-location = %q, want all", cfg.Bar.ButtonGroupBorderLocation)
	}
	if cfg.Bar.ButtonGroupRounding != RoundingLg {
		t.Errorf("button-group-rounding = %q, want lg", cfg.Bar.ButtonGroupRounding)
	}
}

func TestLoadFileRejectsBadColorsAndBorders(t *testing.T) {
	for _, content := range []string{
		"[bar]\nbg = \"not-a-token\"\n",
		"[bar]\nborder-color = \"#zz\"\n",
		"[bar]\nborder-location = \"sideways\"\n",
		"[bar]\nborder-width = 999\n",
		"[bar]\nbutton-group-opacity = 120\n",
		"[bar]\nbutton-group-rounding = \"xl\"\n",
		"[bar]\nbutton-group-border-location = \"sideways\"\n",
	} {
		path := writeConfig(t, content)
		if _, err := LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}

func TestSizeResolvePx(t *testing.T) {
	multiplier := Size{Value: 0.5, Unit: SizeMultiplier}
	if got := multiplier.ResolvePx(16, 2); got != 16 {
		t.Errorf("0.5x at rem 16 scale 2 = %v, want 16", got)
	}
	pixels := Size{Value: 4, Unit: SizePixels}
	if got := pixels.ResolvePx(16, 2); got != 4 {
		t.Errorf("4px at any scale = %v, want 4 (pixels ignore scale)", got)
	}
	if !(Size{Value: 0, Unit: SizeMultiplier}).IsZero() {
		t.Error("zero size: IsZero = false, want true")
	}
}
