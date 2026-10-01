package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// dropdownHeader is the DropdownHeader template: the icon, the title
// (taking the free width), and any trailing actions.
func dropdownHeader(ctx ModuleContext, font render.Font, px float64, icon, title string, actions ...widget.Widget) *widget.Box {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("dropdown-header")
	glyph := widget.NewThemeIcon(icon, int(px*1.2))
	glyph.SetTint(ctx.Style.fg)
	row.Append(glyph, false)
	row.Append(widget.NewLabel(font, px*1.1, title, ctx.Style.fg), true)
	for _, a := range actions {
		row.Append(a, false)
	}
	return row
}

// emptyState is the EmptyState template: a muted icon over the title
// and the wrapped description. An empty description is omitted.
func emptyState(ctx ModuleContext, font render.Font, px float64, icon, title, description string) *widget.Box {
	col := widget.NewBox(widget.Column, 6, 14)
	col.AddClass("empty-state")
	glyph := widget.NewThemeIcon(icon, int(px*2))
	glyph.SetTint(mutedFg(ctx.Style.palette))
	col.Append(glyph, false)
	col.Append(widget.NewLabel(font, px*1.1, title, ctx.Style.fg), false)
	if description != "" {
		desc := widget.NewLabel(font, px*0.9, description, mutedFg(ctx.Style.palette))
		desc.SetWrap(true)
		col.Append(desc, false)
	}
	return col
}
