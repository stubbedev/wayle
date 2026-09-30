package bar

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// iconPx is the bar's icon size in logical pixels; the label height
// scales around it.
const iconPx = 16

// moduleIcon builds the theme icon a module shows beside its label;
// nil when the module shows none or the name is empty. The glyph is
// the bar button's image: its color and size come from the
// stylesheet (--bar-btn-icon-color, --bar-btn-icon-size), as in GTK.
// The concrete return type keeps nil checks honest: a nil
// *widget.Icon never hides inside a non-nil interface.
func moduleIcon(_ ModuleContext, icon config.IconConfig) *widget.Icon {
	if !icon.Show || icon.Name == "" {
		return nil
	}
	return widget.NewThemeIcon(icon.Name, iconPx)
}

// assembleModule builds the module's bar button around its icon (nil
// when it shows none) and its label. The icon is the caller's own
// instance, so state-icon swaps (SetThemeName) land on the widget in
// the tree.
func assembleModule(ctx ModuleContext, ic *widget.Icon, label *widget.Label) widget.Widget {
	return newBarButton(ctx, ic, label)
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
