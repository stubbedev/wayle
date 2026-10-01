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
