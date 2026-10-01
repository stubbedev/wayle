package bar

import (
	"context"
	"log"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// dropdownHeader is the DropdownHeader template: the icon, the title
// (taking the free width), and any trailing actions.
func dropdownHeader(ctx ModuleContext, font render.Font, px float64, icon, title string, actions ...widget.Widget) *widget.Box {
	row, _ := dropdownHeaderIcon(ctx, font, px, icon, title, actions...)
	return row
}

// dropdownHeaderIcon is dropdownHeader that also hands back its icon,
// for headers whose icon follows state.
func dropdownHeaderIcon(ctx ModuleContext, font render.Font, px float64, icon, title string, actions ...widget.Widget) (*widget.Box, *widget.Icon) {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("dropdown-header")
	glyph := widget.NewThemeIcon(icon, int(px*1.2))
	glyph.SetTint(ctx.Style.fg)
	row.Append(glyph, false)
	row.Append(widget.NewLabel(font, px*1.1, title, ctx.Style.fg), true)
	for _, a := range actions {
		row.Append(a, false)
	}
	return row, glyph
}

// emptyState is the EmptyState template: a muted icon over the title
// and the wrapped description. An empty description is omitted.
func emptyState(ctx ModuleContext, font render.Font, px float64, icon, title, description string) *widget.Box {
	col, _ := emptyStateIcon(ctx, font, px, icon, title, description)
	return col
}

// emptyStateIcon is emptyState that also hands back its icon.
func emptyStateIcon(ctx ModuleContext, font render.Font, px float64, icon, title, description string) (*widget.Box, *widget.Icon) {
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
	return col, glyph
}

// followTicks is a dropdown's watcher: on each tick from subscribe it
// reads off the loop and applies the result on it, until life ends or
// the tick channel closes. A failed subscribe logs and follows nothing.
func followTicks[T any](
	mc ModuleContext,
	life context.Context,
	what string,
	subscribe func(context.Context) (<-chan struct{}, func(), error),
	read func(context.Context) T,
	apply func(T),
) {
	ticks, stop, err := subscribe(life)
	if err != nil {
		log.Printf("%s: subscribe: %v", what, err)
		return
	}
	go func() {
		defer stop()
		for {
			select {
			case <-life.Done():
				return
			case _, ok := <-ticks:
				if !ok {
					return
				}
			}
			value := read(life)
			if life.Err() != nil {
				return
			}
			mc.Invoke(func() { apply(value) })
		}
	}()
}

// dropdownButton is a flat dropdown button: the hover and pressed
// fills of the bar style around child.
func dropdownButton(ctx ModuleContext, child widget.Widget, class string, onClick func()) *widget.Button {
	b := widget.NewButton(child, 6, 8)
	b.AddClass(class)
	b.BgHover = ctx.Style.buttonBgHover
	b.BgPressed = ctx.Style.buttonBgActive
	b.OnClick = onClick
	return b
}
