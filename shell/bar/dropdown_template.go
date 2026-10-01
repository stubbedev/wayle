package bar

import (
	"context"
	"log"
	"sync"

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
	row, glyph, _ := dropdownHeaderParts(ctx, font, px, icon, title, actions...)
	return row, glyph
}

// dropdownHeaderParts is dropdownHeader with its icon and title label,
// for headers whose icon or title follows state.
func dropdownHeaderParts(ctx ModuleContext, font render.Font, px float64, icon, title string, actions ...widget.Widget) (*widget.Box, *widget.Icon, *widget.Label) {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("dropdown-header")
	glyph := widget.NewThemeIcon(icon, int(px*1.2))
	glyph.SetTint(ctx.Style.fg)
	row.Append(glyph, false)
	label := widget.NewLabel(font, px*1.1, title, ctx.Style.fg)
	label.SetEllipsize(widget.EllipsizeEnd)
	row.Append(label, true)
	for _, a := range actions {
		row.Append(a, false)
	}
	return row, glyph, label
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

// refresher is a view's reader: every refresh of the view goes through
// it, reads run off the loop one at a time, and requests made while a
// read runs coalesce into one more read after it. Results apply on the
// loop in the order their reads started, so a read that started before
// a newer one can never land last and undo the newer state, and the
// state applied last is never older than the last request.
type refresher[T any] struct {
	mc    ModuleContext
	life  context.Context
	read  func(context.Context) T
	apply func(T)

	mu      sync.Mutex
	running bool
	again   bool
	// thens run on the loop after the apply of the next read to start.
	thens []func()
}

func newRefresher[T any](mc ModuleContext, life context.Context, read func(context.Context) T, apply func(T)) *refresher[T] {
	return &refresher[T]{mc: mc, life: life, read: read, apply: apply}
}

// request asks for a fresh read; safe from any goroutine.
func (r *refresher[T]) request() { r.requestThen(nil) }

// requestThen asks for a fresh read and runs then on the loop once it
// has been applied (nil: nothing).
func (r *refresher[T]) requestThen(then func()) {
	r.mu.Lock()
	if then != nil {
		r.thens = append(r.thens, then)
	}
	if r.running {
		r.again = true
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()
	go r.loop()
}

func (r *refresher[T]) loop() {
	for {
		r.mu.Lock()
		thens := r.thens
		r.thens = nil
		r.mu.Unlock()
		value := r.read(r.life)
		if r.life.Err() != nil {
			r.mu.Lock()
			r.running, r.again = false, false
			r.mu.Unlock()
			return
		}
		r.mc.Invoke(func() {
			r.apply(value)
			for _, fn := range thens {
				fn()
			}
		})
		r.mu.Lock()
		if !r.again {
			r.running = false
			r.mu.Unlock()
			return
		}
		r.again = false
		r.mu.Unlock()
	}
}

// followTicks is a dropdown's watcher: each tick from subscribe asks
// the view's refresher for a read, until life ends or the tick channel
// closes. It returns the refresher, which the view's own refreshes
// (after a write) go through too. A failed subscribe logs and follows
// nothing; the refresher still serves the view's requests.
func followTicks[T any](
	mc ModuleContext,
	life context.Context,
	what string,
	subscribe func(context.Context) (<-chan struct{}, func(), error),
	read func(context.Context) T,
	apply func(T),
) *refresher[T] {
	r := newRefresher(mc, life, read, apply)
	followInto(life, what, subscribe, r)
	return r
}

// followInto feeds subscribe's ticks into an existing refresher, for a
// view that follows several sources with one reader.
func followInto[T any](life context.Context, what string, subscribe func(context.Context) (<-chan struct{}, func(), error), r *refresher[T]) {
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
			r.request()
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

// dropdownScroll is a dropdown's ScrolledWindow: vertical only, as
// every Rust dropdown sets hscrollbar-policy never, so its content is
// the dropdown's width and wrapping or ellipsized text fits it. class
// is the scroll's style class, "" for none.
func dropdownScroll(child widget.Widget, class string) *widget.Scroll {
	s := widget.NewScroll(child)
	s.VerticalOnly = true
	if class != "" {
		s.AddClass(class)
	}
	return s
}
