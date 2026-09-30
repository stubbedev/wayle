package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// iconPx is the bar's icon size in logical pixels; the label height
// scales around it.
const iconPx = 16

// moduleIcon builds the theme icon a module shows beside its label;
// nil when the module shows none or the name is empty. Symbolic
// glyphs recolor through SetTint to the configured color (auto
// resolves to the bar fg). The concrete return type keeps nil checks
// honest: a nil *widget.Icon never hides inside a non-nil interface.
func moduleIcon(ctx ModuleContext, icon config.IconConfig) *widget.Icon {
	if !icon.Show || icon.Name == "" {
		return nil
	}
	ic := widget.NewThemeIcon(icon.Name, iconPx)
	ic.SetTint(moduleIconTint(ctx, icon.Color))
	return ic
}

// moduleIconTint resolves an icon color; auto (and anything that does
// not resolve) is the bar fg.
func moduleIconTint(ctx ModuleContext, cv config.ColorValue) render.Color {
	if resolved, ok := styling.ResolveColor(cv, ctx.Style.palette); ok {
		return resolved
	}
	return ctx.Style.fg
}

// assembleModule pairs the module's icon (nil when it shows none) with
// its label; without an icon the label stays the whole root, which
// keeps single-label modules cheap and their tests simple. The icon is
// the caller's own instance, so state-icon swaps (SetThemeName) land
// on the widget in the tree.
func assembleModule(ctx ModuleContext, ic *widget.Icon, label *widget.Label) widget.Widget {
	if ic == nil {
		return label
	}
	row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
	row.Append(ic, false)
	row.Append(label, false)
	return row
}

// levelIndexFloor is battery helpers.rs's level math: the percentage
// divided evenly across n icons from the top, empty first.
func levelIndexFloor(percent float64, n int) int {
	if n <= 0 {
		return 0
	}
	idx := int(percent / 100.0 * float64(n))
	return min(idx, n-1)
}

// levelIndexSpan is volume helpers.rs's level math: with n icons,
// percent 0 takes the first and 1..100 divide the rest evenly, so
// 1-33% lands on icons[0] with n=3, 34-66% on icons[1], 67-100% on
// icons[2].
func levelIndexSpan(percent int, n int) int {
	if n <= 0 {
		return 0
	}
	if percent <= 0 {
		return 0
	}
	step := 100.0 / float64(n)
	idx := int((float64(percent) - 1.0) / step)
	return min(idx, n-1)
}
