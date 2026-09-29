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

// barStyle is one bar's fully resolved chrome: every config size and
// color already converted to pixels and palette colors, so the widget
// builders never see config types.
type barStyle struct {
	palette *styling.Palette

	bg          render.Color
	fg          render.Color
	border      render.Color
	borders     borderWidths
	radius      int
	insetEdge   int
	insetEnds   int
	padding     int
	paddingEnds int
	moduleGap   int
	labelPx     float64

	groupBg      render.Color
	groupBorder  render.Color
	groupBorders borderWidths
	groupRadius  int
	groupPadding int
	groupGap     int
}

// computeStyle resolves the bar chrome: sizes through Size::resolve_px
// (rem base times bar scale, pixels literal) and colors through the
// token table, with the background mixed to the configured opacity
// exactly as .bar's color-mix does in the Rust shell.
func computeStyle(cfg *config.Config, palette *styling.Palette) barStyle {
	bar := cfg.Bar
	scale := bar.Scale
	px := func(s config.Size) int {
		return int(math.Round(s.ResolvePx(styling.RemBase, scale)))
	}
	resolve := func(cv config.ColorValue) render.Color {
		color, ok := styling.ResolveColor(cv, palette)
		if !ok {
			return render.Color(0)
		}
		return color
	}

	// color-mix(in srgb, var(--bar-bg) var(--bar-opacity), transparent)
	bg := styling.ColorMix(resolve(bar.BG), transparentColor, bar.BackgroundOpacity)

	// The module label size: pixels are literal; multipliers scale the
	// SCSS base ($base-btn-label-size: 1.04rem) by the bar scale and the
	// configured multiplier.
	labelSize := bar.ButtonLabelSize
	var labelPx float64
	if labelSize.Unit == config.SizePixels {
		labelPx = labelSize.Value
	} else {
		labelPx = buttonLabelBaseRem * styling.RemBase * scale * labelSize.Value
	}

	// Group padding keeps the historical 0.25 rem fine-tuning factor
	// for multipliers (styling.rs's match on button-group-padding);
	// pixels are literal.
	var groupPadding int
	if bar.ButtonGroupPadding.Unit == config.SizePixels {
		groupPadding = int(math.Round(bar.ButtonGroupPadding.Value))
	} else {
		groupPadding = int(math.Round(bar.ButtonGroupPadding.Value * 0.25 * styling.RemBase * scale))
	}

	return barStyle{
		palette:     palette,
		bg:          bg,
		fg:          resolve(mustToken(config.TokenFgDefault)),
		border:      resolve(bar.BorderColor),
		borders:     borderWidths{}.fromLocation(bar.BorderLocation, bar.BorderWidth),
		radius:      styling.RoundingRadiusPx(bar.Rounding, scale),
		insetEdge:   px(bar.InsetEdge),
		insetEnds:   px(bar.InsetEnds),
		padding:     px(bar.Padding),
		paddingEnds: px(bar.PaddingEnds),
		moduleGap:   px(bar.ModuleGap),
		labelPx:     labelPx,

		groupBg:      styling.ColorMix(resolve(bar.ButtonGroupBackground), transparentColor, bar.ButtonGroupOpacity),
		groupBorder:  resolve(bar.ButtonGroupBorderColor),
		groupBorders: borderWidths{}.fromLocation(bar.ButtonGroupBorderLocation, bar.ButtonGroupBorderWidth),
		groupRadius:  styling.RoundingRadiusPx(bar.ButtonGroupRounding, scale),
		groupPadding: groupPadding,
		groupGap:     px(bar.ButtonGroupModuleGap),
	}
}

// margins maps the insets onto layer-shell margins for one location:
// the edge inset backs off the docked edge, the ends inset backs off
// the stretch edges. The shadow margin is part of the shadow preset and
// lands with that port.
func (s barStyle) margins(location config.Location) [4]int32 {
	edge := int32(s.insetEdge)
	ends := int32(s.insetEnds)
	switch location {
	case config.LocationTop:
		return [4]int32{edge, ends, 0, ends} // top, right, bottom, left
	case config.LocationBottom:
		return [4]int32{0, ends, edge, ends}
	case config.LocationLeft:
		return [4]int32{ends, edge, ends, 0}
	case config.LocationRight:
		return [4]int32{ends, 0, ends, edge}
	}
	return [4]int32{}
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
