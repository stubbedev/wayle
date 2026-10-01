package bar

import (
	"math"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// borderWidths is a per-side border width set resolved from a
// BorderLocation.
type borderWidths struct {
	left, top, right, bottom int
}

// insets converts the widths to gelm's per-side insets.
func (b borderWidths) insets() render.Insets {
	return render.Insets{Top: b.top, Right: b.right, Bottom: b.bottom, Left: b.left}
}

// any reports whether any edge carries width.
func (b borderWidths) any() bool {
	return b.left > 0 || b.top > 0 || b.right > 0 || b.bottom > 0
}

// fromLocation maps a BorderLocation onto edges the way bar/styling.rs
// expands it: none means no edge, all means every edge.
func (b borderWidths) fromLocation(location config.BorderLocation, width int) borderWidths {
	switch location {
	case config.BorderTop:
		return borderWidths{top: width}
	case config.BorderBottom:
		return borderWidths{bottom: width}
	case config.BorderLeft:
		return borderWidths{left: width}
	case config.BorderRight:
		return borderWidths{right: width}
	case config.BorderAll:
		return borderWidths{left: width, top: width, right: width, bottom: width}
	}
	return borderWidths{}
}

// barStyle is what the Go-painted surfaces (the dropdown panels, the
// workspace buttons, the popups) read to match the bar: the resolved
// palette, the text ink and size, and the dropdown button shades. The
// bar itself is styled by the Rust stylesheet (barTheme); nothing here
// re-derives its cascade.
type barStyle struct {
	palette *styling.Palette

	// fg is the default ink. Bar modules get a copy with fg zero (unset,
	// so the stylesheet's --bar-btn-label-color applies).
	fg        render.Color
	moduleGap int
	labelPx   float64

	buttonBgHover  render.Color
	buttonBgActive render.Color
}

// computeStyle resolves the Go-painted surfaces' parameters: sizes
// through Size::resolve_px (rem base times bar scale, pixels literal)
// and colors through the token table.
func computeStyle(cfg *config.Config, palette *styling.Palette) barStyle {
	bar := cfg.Bar
	scale := float64(bar.Scale)
	resolve := func(cv config.ColorValue) render.Color {
		color, ok := styling.ResolveColor(cv, palette)
		if !ok {
			return render.Color(0)
		}
		return color
	}

	// The module label size: pixels are literal; multipliers scale the
	// SCSS base ($base-btn-label-size: 1.04rem) by the bar scale and the
	// configured multiplier.
	labelSize := bar.ButtonLabelSize
	var labelPx float64
	if labelSize.Unit == config.SizePixels {
		labelPx = float64(labelSize.Value)
	} else {
		labelPx = buttonLabelBaseRem * styling.RemBase * scale * float64(labelSize.Value)
	}

	// The dropdown buttons' hover and active shades deepen the group
	// background at the button bg opacity.
	buttonBase := resolve(bar.ButtonGroupBackground)
	buttonBg := styling.ColorMix(buttonBase, transparentColor, int(bar.ButtonBGOpacity))
	hoverBase, activeBase := buttonBase, buttonBase

	return barStyle{
		palette:        palette,
		fg:             resolve(mustToken(config.TokenFgDefault)),
		moduleGap:      int(math.Round(bar.ModuleGap.ResolvePx(styling.RemBase, scale))),
		labelPx:        labelPx,
		buttonBgHover:  styling.ColorMix(hoverBase, buttonBg, 50),
		buttonBgActive: styling.ColorMix(activeBase, buttonBg, 80),
	}
}

// buttonLabelBaseRem is tokens.scss's $base-btn-label-size, the base
// the button-label-size multiplier scales.
const buttonLabelBaseRem = 1.04

// mustToken wraps a literal token lookup; a typo in one of these
// constants is a programming error, not a config error.
func mustToken(t config.CssToken) config.ColorValue {
	return config.ColorValue{Kind: config.ColorToken, Token: t}
}

var transparentColor = render.Color(0)
