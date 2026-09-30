package config

import (
	"strings"
	"testing"
)

func TestShadowPresetCSS(t *testing.T) {
	for _, tc := range []struct {
		preset   ShadowPreset
		location Location
		want     string
	}{
		{ShadowNone, LocationTop, "none"},
		{ShadowNone, LocationLeft, "none"},
		{ShadowDrop, LocationTop, "0 1px 2px 1px rgba(0, 0, 0, 0.25)"},
		{ShadowDrop, LocationBottom, "0 -1px 2px 1px rgba(0, 0, 0, 0.25)"},
		{ShadowDrop, LocationLeft, "1px 0 2px 1px rgba(0, 0, 0, 0.25)"},
		{ShadowDrop, LocationRight, "-1px 0 2px 1px rgba(0, 0, 0, 0.25)"},
		{ShadowFloating, LocationBottom, "0 1px 2px 1px rgba(0, 0, 0, 0.25)"},
		{ShadowFloating, LocationRight, "0 1px 2px 1px rgba(0, 0, 0, 0.25)"},
	} {
		if got := tc.preset.CSSShadow(tc.location); got != tc.want {
			t.Errorf("%s at %s = %q, want %q", tc.preset, tc.location, got, tc.want)
		}
	}
	for preset, margin := range map[ShadowPreset]int{ShadowNone: 0, ShadowDrop: 4, ShadowFloating: 4} {
		if preset.OppositeMargin() != margin || preset.MarginPx() != margin {
			t.Errorf("%s margin = %d, want %d", preset, preset.OppositeMargin(), margin)
		}
	}
}

func TestStylingEnumCSSMappings(t *testing.T) {
	for v, want := range map[BarButtonVariant]string{
		ButtonVariantBasic: "basic", ButtonVariantBlockPrefix: "block-prefix", ButtonVariantIconSquare: "icon-square",
	} {
		if v.CSSClass() != want {
			t.Errorf("%s class = %q", v, v.CSSClass())
		}
	}
	if class, ok := IconEnd.CSSClass(); !ok || class != "icon-end" {
		t.Errorf("end class = %q/%v", class, ok)
	}
	if _, ok := IconStart.CSSClass(); ok {
		t.Error("start must add no class")
	}
	for w, want := range map[FontWeightClass]string{
		WeightNormal: "--weight-normal", WeightMedium: "--weight-medium",
		WeightSemibold: "--weight-semibold", WeightBold: "--weight-bold",
	} {
		if w.CSSVar() != want || w.CSSClass() != strings.TrimPrefix(want, "--") {
			t.Errorf("%s = %q/%q", w, w.CSSVar(), w.CSSClass())
		}
	}
	for b, want := range map[BorderLocation]string{
		BorderTop: "border-top", BorderBottom: "border-bottom", BorderLeft: "border-left",
		BorderRight: "border-right", BorderAll: "border-all",
	} {
		if class, ok := b.CSSClass(); !ok || class != want {
			t.Errorf("%s class = %q/%v", b, class, ok)
		}
	}
	if _, ok := BorderNone.CSSClass(); ok {
		t.Error("border none must add no class")
	}
}

func TestRoundingCSSValues(t *testing.T) {
	r := func(e, c string) RoundingCSSValues { return RoundingCSSValues{"var(" + e + ")", "var(" + c + ")"} }
	for _, tc := range []struct {
		level                 RoundingLevel
		global, bar, barElems RoundingCSSValues
	}{
		{
			RoundingNone,
			r("--radius-none", "--radius-none"), r("--radius-none", "--radius-none"), r("--radius-none", "--radius-none"),
		},
		{
			RoundingSm,
			r("--radius-sm", "--radius-md"), r("--bar-radius-sm", "--bar-radius-md"),
			r("--bar-button-radius-sm", "--bar-button-radius-md"),
		},
		{
			RoundingMd,
			r("--radius-md", "--radius-lg"), r("--bar-radius-md", "--bar-radius-lg"),
			r("--bar-button-radius-md", "--bar-button-radius-lg"),
		},
		{
			RoundingLg,
			r("--radius-lg", "--radius-xl"), r("--bar-radius-lg", "--bar-radius-xl"),
			r("--bar-button-radius-lg", "--bar-button-radius-xl"),
		},
		{
			RoundingFull,
			r("--radius-full", "--radius-xl"), r("--radius-full", "--radius-full"), r("--radius-full", "--radius-full"),
		},
	} {
		if got := tc.level.CSSValues(); got != tc.global {
			t.Errorf("%s CSSValues = %+v, want %+v", tc.level, got, tc.global)
		}
		if got := tc.level.BarCSSValues(); got != tc.bar {
			t.Errorf("%s BarCSSValues = %+v, want %+v", tc.level, got, tc.bar)
		}
		if got := tc.level.BarElementCSSValues(); got != tc.barElems {
			t.Errorf("%s BarElementCSSValues = %+v, want %+v", tc.level, got, tc.barElems)
		}
	}
}

func TestSizeScaleAndPxValue(t *testing.T) {
	scale := Size{Value: 1.5, Unit: SizeMultiplier}
	px := Size{Value: 24, Unit: SizePixels}
	if v, ok := scale.ScaleValue(); !ok || v != 1.5 {
		t.Errorf("scale ScaleValue = %v/%v", v, ok)
	}
	if _, ok := scale.PxValue(); ok {
		t.Error("a scale has no px value")
	}
	if v, ok := px.PxValue(); !ok || v != 24 {
		t.Errorf("px PxValue = %v/%v", v, ok)
	}
	if _, ok := px.ScaleValue(); ok {
		t.Error("a pixel size has no scale value")
	}
}

func TestBarStylingKeysDefaultsAndDecode(t *testing.T) {
	d := Defaults().Bar
	if d.Shadow != ShadowNone || d.ButtonVariant != ButtonVariantBlockPrefix || d.ButtonOpacity != 100 ||
		d.ButtonIconSize != (Size{1, SizeMultiplier}) || d.ButtonLabelWeight != WeightSemibold ||
		d.ButtonGap != (Size{1, SizeMultiplier}) || d.ButtonIconPosition != IconStart ||
		!d.DropdownShadow || d.DropdownOpacity != 100 {
		t.Errorf("bar styling defaults = %+v", d)
	}
	path := writeConfig(t, `
[bar]
shadow = "drop"
button-variant = "icon-square"
button-opacity = 80
button-bg-opacity = 50
button-icon-size = "20px"
button-label-weight = "bold"
button-gap = 2
button-icon-position = "end"
dropdown-shadow = false
dropdown-opacity = 85
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b := cfg.Bar
	if b.Shadow != ShadowDrop || b.ButtonVariant != ButtonVariantIconSquare || b.ButtonOpacity != 80 ||
		b.ButtonBGOpacity != 50 || b.ButtonIconSize != (Size{20, SizePixels}) || b.ButtonLabelWeight != WeightBold ||
		b.ButtonGap != (Size{2, SizeMultiplier}) || b.ButtonIconPosition != IconEnd ||
		b.DropdownShadow || b.DropdownOpacity != 85 {
		t.Errorf("decoded bar styling = %+v", b)
	}
	// A [bar] table without exclusive keeps the schema default (true).
	if !b.Exclusive {
		t.Error("exclusive flipped to false by a [bar] table that does not set it")
	}
}

func TestBarStylingKeysRejectBadValues(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`shadow = "soft"`, "shadow"},
		{`button-variant = "pill"`, "button-variant"},
		{`button-label-weight = "heavy"`, "font weight"},
		{`button-icon-position = "middle"`, "button-icon-position"},
		{`button-opacity = 101`, "button-opacity"},
		{`button-bg-opacity = -1`, "button-bg-opacity"},
		{`dropdown-opacity = 200`, "dropdown-opacity"},
		{`button-icon-size = "big"`, "button-icon-size"},
		{`button-gap = -1`, "button-gap"},
	} {
		path := writeConfig(t, "[bar]\n"+tc.body+"\n")
		if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.body, err, tc.want)
		}
	}
}

func TestClockTableWithoutFormatKeepsTheDefault(t *testing.T) {
	path := writeConfig(t, "[modules.clock]\nlabel-color = \"red\"\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Clock.Format != Defaults().Clock.Format {
		t.Errorf("format = %q, want the default", cfg.Clock.Format)
	}
	if cfg.Clock.Button.Colors.Label.Token != TokenRed {
		t.Errorf("clock label-color = %+v", cfg.Clock.Button.Colors.Label)
	}
}
