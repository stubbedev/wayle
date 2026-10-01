package styling

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
)

// formatF32 prints a float32 as Rust's f32 Display does: the shortest
// decimal that round-trips, never in exponent form ("1", "1.01").
func formatF32(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }

// formatF64 is Rust's f64 Display.
func formatF64(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// pxOverride is one bar-button size override declaration: a pixel size
// sets "<name>: <n>px;" so the SCSS var(--…-override, calc(…)) prefers
// it; a scale size emits nothing and the calc fallback applies
// (lib.rs px_override).
func pxOverride(name string, size config.Size) string {
	px, ok := size.PxValue()
	if !ok {
		return ""
	}
	return "    " + name + ": " + formatF32(float32(px)) + "px;\n"
}

// scaleOr1 is a size's scale multiplier, or 1 for a pixel size (whose
// override then carries the length).
func scaleOr1(size config.Size) string {
	if v, ok := size.ScaleValue(); ok {
		return formatF32(float32(v))
	}
	return "1"
}

// ThemeCSS is the runtime theme block, byte for byte the Rust theme_css
// (crates/wayle-styling/src/lib.rs): the :root custom properties for the
// palette, the dropdown surface, fonts, scales, the bar-button pixel
// overrides, and the rounding tokens. Rust resolves the palette inside
// theme_css; here the caller passes the result of ResolvePalette, so the
// provider failure surfaces as an error to log instead of a hidden log
// line. The output is the full ":root { ... }" rule.
func ThemeCSS(p config.Palette, general config.GeneralConfig, bar config.BarConfig, s config.StylingConfig) string {
	global := s.Rounding.CSSValues()
	barValues := bar.Rounding.BarCSSValues()
	buttonValues := bar.ButtonRounding.BarElementCSSValues()
	groupValues := bar.ButtonGroupRounding.BarElementCSSValues()
	dropdownOpacity := float32(bar.DropdownOpacity) / 100

	var b strings.Builder
	decl := func(name, value string) { b.WriteString("    " + name + ": " + value + ";\n") }
	b.WriteString(":root {\n")
	decl("--palette-bg", p.Bg)
	decl("--palette-surface", p.Surface)
	decl("--palette-elevated", p.Elevated)
	decl("--palette-fg", p.Fg)
	decl("--palette-fg-muted", p.FgMuted)
	decl("--palette-primary", p.Primary)
	decl("--palette-red", p.Red)
	decl("--palette-yellow", p.Yellow)
	decl("--palette-green", p.Green)
	decl("--palette-blue", p.Blue)
	decl("--dropdown-surface", hexToRGBA(p.Surface, dropdownOpacity))
	b.WriteString("\n")
	decl("--cfg-font-sans", `"`+general.FontSans+`"`)
	decl("--cfg-font-mono", `"`+general.FontMono+`"`)
	b.WriteString("\n")
	decl("--global-scale", formatF32(float32(s.Scale)))
	decl("--bar-scale", formatF32(float32(bar.Scale)))
	decl("--bar-btn-icon-scale", scaleOr1(bar.ButtonIconSize))
	decl("--bar-btn-icon-padding-scale", scaleOr1(bar.ButtonIconPadding))
	decl("--bar-btn-label-scale", scaleOr1(bar.ButtonLabelSize))
	decl("--bar-btn-label-padding-scale", scaleOr1(bar.ButtonLabelPadding))
	decl("--bar-btn-gap-scale", scaleOr1(bar.ButtonGap))
	b.WriteString(pxOverride("--bar-btn-icon-size-override", bar.ButtonIconSize))
	b.WriteString(pxOverride("--bar-btn-icon-padding-override", bar.ButtonIconPadding))
	b.WriteString(pxOverride("--bar-btn-label-size-override", bar.ButtonLabelSize))
	b.WriteString(pxOverride("--bar-btn-label-padding-override", bar.ButtonLabelPadding))
	b.WriteString(pxOverride("--bar-btn-gap-override", bar.ButtonGap))
	b.WriteString("\n")
	decl("--cfg-rounding-element", global.Element)
	decl("--cfg-rounding-container", global.Container)
	decl("--cfg-bar-rounding-element", barValues.Element)
	decl("--cfg-bar-rounding-container", barValues.Container)
	decl("--cfg-bar-button-rounding-element", buttonValues.Element)
	decl("--cfg-bar-group-rounding-element", groupValues.Element)
	b.WriteString("}")
	return b.String()
}

// PaletteFromHex converts a resolved hex palette to the render-color
// Palette the Go painters consume. A provider can hand back any string,
// so an unparsable color is an error naming the slot.
func PaletteFromHex(p config.Palette) (*Palette, error) {
	out := &Palette{}
	for _, slot := range []struct {
		name string
		hex  string
		dst  *render.Color
	}{
		{"bg", p.Bg, &out.Bg},
		{"surface", p.Surface, &out.Surface},
		{"elevated", p.Elevated, &out.Elevated},
		{"fg", p.Fg, &out.Fg},
		{"fg-muted", p.FgMuted, &out.FgMuted},
		{"primary", p.Primary, &out.Primary},
		{"red", p.Red, &out.Red},
		{"yellow", p.Yellow, &out.Yellow},
		{"green", p.Green, &out.Green},
		{"blue", p.Blue, &out.Blue},
	} {
		if _, err := config.ParseHexColor(slot.hex); err != nil {
			return nil, fmt.Errorf("styling: palette %s: %w", slot.name, err)
		}
		c, _ := parseHex(slot.hex) // validated above
		*slot.dst = c
	}
	return out, nil
}
