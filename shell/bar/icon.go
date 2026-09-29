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
// resolves to the bar fg).
func moduleIcon(ctx ModuleContext, icon config.IconConfig) widget.Widget {
	if !icon.Show || icon.Name == "" {
		return nil
	}
	ic := widget.NewThemeIcon(icon.Name, iconPx)
	tint := ctx.Style.fg
	if resolved, ok := styling.ResolveColor(icon.Color, ctx.Style.palette); ok {
		tint = resolved
	}
	ic.SetTint(tint)
	return ic
}

// assembleModule pairs an icon (when configured) with the module's
// label; without an icon the label stays the whole root, which keeps
// single-label modules cheap and their tests simple.
func assembleModule(ctx ModuleContext, icon config.IconConfig, label *widget.Label) widget.Widget {
	ic := moduleIcon(ctx, icon)
	if ic == nil {
		return label
	}
	row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
	row.Append(ic, false)
	row.Append(label, false)
	return row
}

// iconRow is the render.Color helper icons need when a module
// restyles on state; unused icons resolve to nil through moduleIcon.
var _ render.Color
