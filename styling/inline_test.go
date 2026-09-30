package styling

import (
	"testing"

	"github.com/stubbedev/wayle/config"
)

func color(t *testing.T, s string) config.ColorValue {
	t.Helper()
	cv, err := config.ParseColorValue(s)
	if err != nil {
		t.Fatal(err)
	}
	return cv
}

func TestResolveColorCSS(t *testing.T) {
	def := color(t, "bg-surface")
	for _, tc := range []struct {
		value    string
		provider config.ThemeProvider
		want     string
	}{
		{"#123456", config.ThemeWayle, "#123456"},
		{"#123456", config.ThemeMatugen, "var(--bg-surface)"}, // a fixed hex has no mapping
		{"#123456", config.ThemePywal, "var(--bg-surface)"},
		{"accent", config.ThemeWallust, "var(--accent)"}, // tokens follow any palette
		{"transparent", config.ThemeMatugen, "transparent"},
		{"auto", config.ThemeMatugen, "var(--accent)"},
	} {
		if got := ResolveColorCSS(color(t, tc.value), def, tc.provider); got != tc.want {
			t.Errorf("%s under %s = %q, want %q", tc.value, tc.provider, got, tc.want)
		}
	}
}

func TestBarCSSDefaults(t *testing.T) {
	want := ".bar { --bar-scale: 1; --bar-bg: var(--bg-surface); --bar-opacity: 100%; " +
		"--bar-border-color: var(--border-accent); --bar-border-top: 0; --bar-border-bottom: 0; " +
		"--bar-border-left: 0; --bar-border-right: 0; --bar-inset-edge-px: 0; --bar-inset-ends-px: 0; " +
		"--bar-padding-px: 6; --bar-padding-ends-px: 8; --bar-module-gap-px: 8; --bar-button-opacity: 1; " +
		"--bar-button-bg-opacity: 100%; --bar-btn-label-weight: var(--weight-semibold); " +
		"--bar-group-module-gap-px: 4; --bar-group-padding-px: 0; --bar-group-bg: var(--bg-elevated); " +
		"--bar-group-opacity: 100%; --bar-group-border-color: var(--border-accent); " +
		"--bar-group-border-top: 0; --bar-group-border-bottom: 0; --bar-group-border-left: 0; " +
		"--bar-group-border-right: 0; --bar-shadow: none; --bar-shadow-margin: 0; }"
	if got := BarCSS(config.Defaults().Bar, config.ThemeWayle); got != want {
		t.Errorf("BarCSS defaults =\n%s\nwant\n%s", got, want)
	}
}

func TestBarCSSConfigured(t *testing.T) {
	bar := config.Defaults().Bar
	bar.Scale = 1.25
	bar.BG = color(t, "#123456")
	bar.BackgroundOpacity = 90
	bar.BorderColor = color(t, "red")
	bar.BorderLocation, bar.BorderWidth = config.BorderLeft, 2
	bar.InsetEdge = config.Size{Value: 3.6, Unit: config.SizePixels}
	bar.InsetEnds = config.Size{Value: 1, Unit: config.SizeMultiplier}
	bar.PaddingEnds = config.Size{Value: 0.15625, Unit: config.SizeMultiplier} // 2.5px: rounds away from zero
	bar.ButtonOpacity, bar.ButtonBGOpacity = 85, 40
	bar.ButtonLabelWeight = config.WeightBold
	bar.ButtonGroupPadding = config.Size{Value: 2, Unit: config.SizeMultiplier}
	bar.ButtonGroupBorderLocation, bar.ButtonGroupBorderWidth = config.BorderAll, 3
	bar.ButtonGroupBackground = color(t, "#abcdef")
	bar.Location, bar.Shadow = config.LocationBottom, config.ShadowDrop

	want := ".bar { --bar-scale: 1.25; --bar-bg: var(--bg-surface); --bar-opacity: 90%; " +
		"--bar-border-color: var(--red); --bar-border-top: 0; --bar-border-bottom: 0; " +
		"--bar-border-left: 2; --bar-border-right: 0; --bar-inset-edge-px: 4; --bar-inset-ends-px: 20; " +
		"--bar-padding-px: 7; --bar-padding-ends-px: 3; --bar-module-gap-px: 10; --bar-button-opacity: 0.85; " +
		"--bar-button-bg-opacity: 40%; --bar-btn-label-weight: var(--weight-bold); " +
		"--bar-group-module-gap-px: 5; --bar-group-padding-px: 10; --bar-group-bg: var(--bg-elevated); " +
		"--bar-group-opacity: 100%; --bar-group-border-color: var(--border-accent); " +
		"--bar-group-border-top: 3; --bar-group-border-bottom: 3; --bar-group-border-left: 3; " +
		"--bar-group-border-right: 3; --bar-shadow: 0 -1px 2px 1px rgba(0, 0, 0, 0.25); --bar-shadow-margin: 4; }"
	// Under matugen the custom hexes fall back to the field defaults.
	if got := BarCSS(bar, config.ThemeMatugen); got != want {
		t.Errorf("BarCSS =\n%s\nwant\n%s", got, want)
	}
	// Under wayle they are used as-is.
	wayle := BarCSS(bar, config.ThemeWayle)
	for _, part := range []string{"--bar-bg: #123456;", "--bar-group-bg: #abcdef;"} {
		if !contains(wayle, part) {
			t.Errorf("wayle BarCSS lacks %q:\n%s", part, wayle)
		}
	}
	bar.ButtonGroupPadding = config.Size{Value: 5.5, Unit: config.SizePixels}
	if got := BarCSS(bar, config.ThemeWayle); !contains(got, "--bar-group-padding-px: 6;") {
		t.Errorf("a pixel group padding is taken literally (rounded):\n%s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestButtonCSS(t *testing.T) {
	d := config.Defaults()
	battery := d.Battery.Button
	bar := d.Bar // block-prefix variant, border width 1

	want := "* { --bar-btn-icon-color: var(--fg-on-accent); --bar-btn-label-color: var(--yellow); " +
		"--bar-btn-icon-bg: var(--yellow); --bar-btn-bg: var(--bg-surface-elevated); " +
		"--bar-btn-border-color: var(--yellow); --bar-btn-border-width: 1px; }"
	if got := ButtonCSS(battery, bar, config.ThemeWayle, config.ThresholdColors{}); got != want {
		t.Errorf("ButtonCSS defaults =\n%s\nwant\n%s", got, want)
	}

	// Basic variant: auto resolves to the module's accent token.
	basic := bar
	basic.ButtonVariant = config.ButtonVariantBasic
	basic.ButtonBorderWidth = 3
	want = "* { --bar-btn-icon-color: var(--yellow); --bar-btn-label-color: var(--yellow); " +
		"--bar-btn-icon-bg: var(--yellow); --bar-btn-bg: var(--bg-surface-elevated); " +
		"--bar-btn-border-color: var(--yellow); --bar-btn-border-width: 3px; }"
	if got := ButtonCSS(battery, basic, config.ThemeWayle, config.ThresholdColors{}); got != want {
		t.Errorf("ButtonCSS basic =\n%s\nwant\n%s", got, want)
	}

	// Threshold overrides win per slot; unset slots keep the config.
	errColor, bg := color(t, "status-error"), color(t, "#ff0000")
	over := config.ThresholdColors{LabelColor: &errColor, ButtonBgColor: &bg}
	want = "* { --bar-btn-icon-color: var(--fg-on-accent); --bar-btn-label-color: var(--status-error); " +
		"--bar-btn-icon-bg: var(--yellow); --bar-btn-bg: #ff0000; " +
		"--bar-btn-border-color: var(--yellow); --bar-btn-border-width: 1px; }"
	if got := ButtonCSS(battery, bar, config.ThemeMatugen, over); got != want {
		t.Errorf("ButtonCSS thresholds =\n%s\nwant\n%s", got, want)
	}

	// Custom hexes: kept under wayle, the schema default under a provider.
	custom := battery
	custom.Colors.Icon, custom.Colors.Label = color(t, "#010203"), color(t, "#040506")
	if got := ButtonCSS(custom, bar, config.ThemeWayle, config.ThresholdColors{}); !contains(got, "--bar-btn-icon-color: #010203;") ||
		!contains(got, "--bar-btn-label-color: #040506;") {
		t.Errorf("wayle keeps custom hexes:\n%s", got)
	}
	if got := ButtonCSS(custom, basic, config.ThemePywal, config.ThresholdColors{}); !contains(got, "--bar-btn-icon-color: var(--yellow);") ||
		!contains(got, "--bar-btn-label-color: var(--yellow);") {
		t.Errorf("a provider falls back to the defaults (auto icon → accent token):\n%s", got)
	}
}

func TestResolveIconColor(t *testing.T) {
	b := config.Defaults().Network.Button
	for _, tc := range []struct {
		icon     string
		variant  config.BarButtonVariant
		provider config.ThemeProvider
		want     string
	}{
		{"auto", config.ButtonVariantBasic, config.ThemeWayle, "var(--accent)"},
		{"auto", config.ButtonVariantBlockPrefix, config.ThemeWayle, "var(--fg-on-accent)"},
		{"auto", config.ButtonVariantIconSquare, config.ThemeWayle, "var(--fg-on-accent)"},
		{"red", config.ButtonVariantIconSquare, config.ThemeWayle, "var(--red)"},
		{"#00ff00", config.ButtonVariantBasic, config.ThemeWayle, "#00ff00"},
		{"#00ff00", config.ButtonVariantBasic, config.ThemeWallust, "var(--accent)"},
		{"transparent", config.ButtonVariantBasic, config.ThemeWayle, "transparent"},
	} {
		b.Colors.Icon = color(t, tc.icon)
		if got := ResolveIconColor(b, tc.variant, tc.provider); got != tc.want {
			t.Errorf("icon %s/%s/%s = %q, want %q", tc.icon, tc.variant, tc.provider, got, tc.want)
		}
	}
}

func TestContainerCSS(t *testing.T) {
	d := config.Defaults()
	c := d.Cava.Container
	want := "* { --bar-container-bg: var(--bg-surface-elevated); --bar-container-border-color: var(--border-accent); " +
		"--bar-container-border-width: 0px; }"
	if got := ContainerCSS(c, d.Bar, config.ThemeWayle); got != want {
		t.Errorf("ContainerCSS defaults =\n%s\nwant\n%s", got, want)
	}
	c.BorderShow = true
	c.Background = color(t, "#222222")
	bar := d.Bar
	bar.ButtonBorderWidth = 2
	want = "* { --bar-container-bg: var(--bg-surface-elevated); --bar-container-border-color: var(--border-accent); " +
		"--bar-container-border-width: 2px; }"
	if got := ContainerCSS(c, bar, config.ThemeMatugen); got != want {
		t.Errorf("ContainerCSS shown border =\n%s\nwant\n%s", got, want)
	}
	if got := ContainerCSS(c, bar, config.ThemeWayle); !contains(got, "--bar-container-bg: #222222;") {
		t.Errorf("wayle keeps the custom background:\n%s", got)
	}
}
