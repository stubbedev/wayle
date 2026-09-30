package styling

import (
	"math"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
)

// The per-widget inline variable blocks. Each function returns the full
// rule text exactly as the Rust component's build_css formats it
// (".bar { ... }" or "* { ... }"), for loading as that widget's own
// stylesheet. They live here rather than in shell/bar because they are
// pure config→CSS functions over the same resolution rules as ThemeCSS.

// ResolveColorCSS is a color field's CSS (resolve_color in
// crates/wayle-widgets/src/styling.rs): tokens, transparent, and auto
// pass through as CSS so a palette swap follows; a custom hex is used
// under the wayle provider but falls back to the field's schema default
// under an extractor, where a fixed hex has no mapping.
func ResolveColorCSS(value, fieldDefault config.ColorValue, provider config.ThemeProvider) string {
	if value.Kind == config.ColorCustom && provider != config.ThemeWayle {
		return fieldDefault.ToCSS()
	}
	return value.ToCSS()
}

// ResolveIconColor is a bar button's icon color (resolve_icon_color in
// crates/wayle-widgets/src/components/bar_buttons/component.rs): the
// custom-hex fallback of ResolveColorCSS, then auto resolves to the
// module's accent token under the basic variant and to fg-on-accent on
// the block-prefix and icon-square chrome.
func ResolveIconColor(b config.ButtonConfig, variant config.BarButtonVariant, provider config.ThemeProvider) string {
	color := b.Colors.Icon
	if color.Kind == config.ColorCustom && provider != config.ThemeWayle {
		color = b.Defaults.Icon
	}
	if !color.IsAuto() {
		return color.ToCSS()
	}
	if variant == config.ButtonVariantBasic {
		return b.AutoIconColor.CSSVar()
	}
	return config.TokenFgOnAccent.CSSVar()
}

// ButtonCSS is one bar button's variable block (bar_buttons/styling.rs
// build_css): the five colors, each a threshold override when one is
// active, and the bar's button border width.
func ButtonCSS(b config.ButtonConfig, bar config.Bar, provider config.ThemeProvider, t config.ThresholdColors) string {
	return "* { " +
		"--bar-btn-icon-color: " + config.ResolveOr(t.IconColor, ResolveIconColor(b, bar.ButtonVariant, provider)) + "; " +
		"--bar-btn-label-color: " + config.ResolveOr(t.LabelColor, ResolveColorCSS(b.Colors.Label, b.Defaults.Label, provider)) + "; " +
		"--bar-btn-icon-bg: " + config.ResolveOr(t.IconBackground, ResolveColorCSS(b.Colors.IconBg, b.Defaults.IconBg, provider)) + "; " +
		"--bar-btn-bg: " + config.ResolveOr(t.ButtonBackground, ResolveColorCSS(b.Colors.ButtonBg, b.Defaults.ButtonBg, provider)) + "; " +
		"--bar-btn-border-color: " + config.ResolveOr(t.BorderColor, ResolveColorCSS(b.Colors.Border, b.Defaults.Border, provider)) + "; " +
		"--bar-btn-border-width: " + strconv.Itoa(bar.ButtonBorderWidth) + "px; " +
		"}"
}

// ContainerCSS is a bar container's variable block
// (bar_container/styling.rs build_css): background, border color, and
// the bar's button border width, zero while border-show is off.
func ContainerCSS(c config.ContainerConfig, bar config.Bar, provider config.ThemeProvider) string {
	width := 0
	if c.BorderShow {
		width = bar.ButtonBorderWidth
	}
	return "* { " +
		"--bar-container-bg: " + ResolveColorCSS(c.Background, c.DefaultBackground, provider) + "; " +
		"--bar-container-border-color: " + ResolveColorCSS(c.BorderColor, c.DefaultBorderColor, provider) + "; " +
		"--bar-container-border-width: " + strconv.Itoa(width) + "px; " +
		"}"
}

// barRemBase is the bar's px per rem (bar/styling.rs REM_BASE).
const barRemBase float32 = 16

// remToPxRounded rounds rem × scale × 16 to whole pixels, keeping icons
// on the pixel grid (rem_to_px_rounded).
func remToPxRounded(rem, scale float32) int {
	return int(math.Round(float64(float32(float32(rem*scale) * barRemBase))))
}

// sizeToPxRounded resolves a size to whole pixels: a multiplier is
// 16 × m × scale, pixels are literal (size_to_px_rounded).
func sizeToPxRounded(size config.Size, scale float32) int {
	if px, ok := size.PxValue(); ok {
		return int(math.Round(float64(float32(px))))
	}
	m := float32(size.Value)
	return int(math.Round(float64(float32(float32(barRemBase*m) * scale))))
}

// borderSides splits a border placement into top/bottom/left/right
// widths.
func borderSides(location config.BorderLocation, width int) (top, bottom, left, right int) {
	switch location {
	case config.BorderTop:
		return width, 0, 0, 0
	case config.BorderBottom:
		return 0, width, 0, 0
	case config.BorderLeft:
		return 0, 0, width, 0
	case config.BorderRight:
		return 0, 0, 0, width
	case config.BorderAll:
		return width, width, width, width
	}
	return 0, 0, 0, 0
}

// BarCSS is the bar's variable block (crates/wayle-shell/src/shell/bar/
// styling.rs build_css): scale, colors, borders, the pixel-rounded
// spacing, the button opacity and weight, the group chrome, and the
// shadow.
func BarCSS(bar config.Bar, provider config.ThemeProvider) string {
	defaults := config.Defaults().Bar
	scale := float32(bar.Scale)
	top, bottom, left, right := borderSides(bar.BorderLocation, bar.BorderWidth)
	gTop, gBottom, gLeft, gRight := borderSides(bar.ButtonGroupBorderLocation, bar.ButtonGroupBorderWidth)
	var groupPadding int
	if px, ok := bar.ButtonGroupPadding.PxValue(); ok {
		groupPadding = int(math.Round(float64(float32(px))))
	} else {
		// Scale keeps the historical 0.25 rem fine-tuning factor.
		groupPadding = remToPxRounded(float32(float32(bar.ButtonGroupPadding.Value)*0.25), scale)
	}
	itoa := strconv.Itoa

	var b strings.Builder
	decl := func(name, value string) { b.WriteString(name + ": " + value + "; ") }
	b.WriteString(".bar { ")
	decl("--bar-scale", formatF32(scale))
	decl("--bar-bg", ResolveColorCSS(bar.BG, defaults.BG, provider))
	decl("--bar-opacity", itoa(bar.BackgroundOpacity)+"%")
	decl("--bar-border-color", ResolveColorCSS(bar.BorderColor, defaults.BorderColor, provider))
	decl("--bar-border-top", itoa(top))
	decl("--bar-border-bottom", itoa(bottom))
	decl("--bar-border-left", itoa(left))
	decl("--bar-border-right", itoa(right))
	decl("--bar-inset-edge-px", itoa(sizeToPxRounded(bar.InsetEdge, scale)))
	decl("--bar-inset-ends-px", itoa(sizeToPxRounded(bar.InsetEnds, scale)))
	decl("--bar-padding-px", itoa(sizeToPxRounded(bar.Padding, scale)))
	decl("--bar-padding-ends-px", itoa(sizeToPxRounded(bar.PaddingEnds, scale)))
	decl("--bar-module-gap-px", itoa(sizeToPxRounded(bar.ModuleGap, scale)))
	decl("--bar-button-opacity", formatF64(float64(bar.ButtonOpacity)/100))
	decl("--bar-button-bg-opacity", itoa(bar.ButtonBGOpacity)+"%")
	decl("--bar-btn-label-weight", "var("+bar.ButtonLabelWeight.CSSVar()+")")
	decl("--bar-group-module-gap-px", itoa(sizeToPxRounded(bar.ButtonGroupModuleGap, scale)))
	decl("--bar-group-padding-px", itoa(groupPadding))
	decl("--bar-group-bg", ResolveColorCSS(bar.ButtonGroupBackground, defaults.ButtonGroupBackground, provider))
	decl("--bar-group-opacity", itoa(bar.ButtonGroupOpacity)+"%")
	decl("--bar-group-border-color", ResolveColorCSS(bar.ButtonGroupBorderColor, defaults.ButtonGroupBorderColor, provider))
	decl("--bar-group-border-top", itoa(gTop))
	decl("--bar-group-border-bottom", itoa(gBottom))
	decl("--bar-group-border-left", itoa(gLeft))
	decl("--bar-group-border-right", itoa(gRight))
	decl("--bar-shadow", bar.Shadow.CSSShadow(bar.Location))
	decl("--bar-shadow-margin", itoa(bar.Shadow.OppositeMargin()))
	b.WriteString("}")
	return b.String()
}
