package bar

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// popoverCard is the popover root the stylesheet styles through: the
// popover element carrying GTK's dropdown classes, a contents node
// under it (popover.dropdown > contents .dropdown takes the surface
// color), and the card inside. gravity picks the shadow-direction
// class the position rules read; shadow gates the shadow class, the
// bar's dropdown-shadow config (default on).
func popoverCard(content widget.Widget, gravity app.Gravity, shadow bool) *widget.Box {
	card := widget.NewBox(widget.Column, 0, 0)
	card.SetElement("popover")
	classes := []string{"dropdown"}
	if shadow {
		classes = append(classes, "shadow")
	}
	classes = append(classes, positionClass(gravity))
	card.AddClass(classes...)
	contents := widget.NewBox(widget.Column, 0, 0)
	contents.AddClass("contents")
	card.Append(contents, true)
	contents.Append(content, true)
	return card
}

// positionClass names the popover's opened-toward edge, GTK's
// position-* classes: a bar at the bottom opens its dropdown upward.
func positionClass(gravity app.Gravity) string {
	switch gravity {
	case app.GravityTop:
		return "position-top"
	case app.GravityLeft:
		return "position-left"
	case app.GravityRight:
		return "position-right"
	}
	return "position-bottom"
}

// dropdownHeader is the DropdownHeader template: the icon, the title
// (taking the free width), and any trailing actions.
func dropdownHeader(font render.Font, px float64, icon, title string, actions ...widget.Widget) *widget.Box {
	row, _ := dropdownHeaderIcon(font, px, icon, title, actions...)
	return row
}

// dropdownHeaderIcon is dropdownHeader that also hands back its icon,
// for headers whose icon follows state.
func dropdownHeaderIcon(font render.Font, px float64, icon, title string, actions ...widget.Widget) (*widget.Box, *widget.Icon) {
	row, glyph, _ := dropdownHeaderParts(font, px, icon, title, actions...)
	return row, glyph
}

// dropdownHeaderParts is dropdownHeader with its icon and title label,
// for headers whose icon or title follows state. The tree carries the
// stylesheet's classes: .dropdown-header paints the strip (padding,
// elevated background, bottom border) and .dropdown-title paints the
// label (size, weight, ink) and its image (accent, size, right
// margin), so nothing here colors itself. The title's natural width
// caps at 24 characters (the Rust header's max-width-chars) and the
// actions box exists even when empty, the node tree the CSS expects.
func dropdownHeaderParts(font render.Font, px float64, icon, title string, actions ...widget.Widget) (*widget.Box, *widget.Icon, *widget.Label) {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("dropdown-header")
	titleBox := widget.NewBox(widget.Row, 0, 0)
	titleBox.AddClass("dropdown-title")
	glyph := widget.NewThemeIcon(icon, int(px*1.2))
	titleBox.Append(glyph, false)
	label := widget.NewLabel(font, px*1.1, title, 0)
	label.SetEllipsize(widget.EllipsizeEnd)
	label.SetMaxWidthChars(24)
	titleBox.Append(label, true)
	row.Append(titleBox, true)
	acts := widget.NewBox(widget.Row, 0, 0)
	acts.AddClass("dropdown-actions")
	for _, a := range actions {
		acts.Append(a, false)
	}
	row.Append(acts, false)
	return row, glyph, label
}

// emptyState is the EmptyState template: a muted icon over the title
// and the wrapped description, spacing-0 with the CSS margins carrying
// the gaps (the Rust template sets none). An empty description still
// renders its (empty) label: the node the stylesheet margins. The
// caller expands it so it centers in its page.
func emptyState(font render.Font, px float64, icon, title, description string) *widget.Box {
	col, _ := emptyStateIcon(font, px, icon, title, description)
	return col
}

// emptyStateIcon is emptyState that also hands back its icon. The
// stylesheet paints it: .empty-state .icon colors and sizes the glyph,
// .title and .description ink the text.
func emptyStateIcon(font render.Font, px float64, icon, title, description string) (*widget.Box, *widget.Icon) {
	return emptyStateSized(font, px, icon, title, description, "")
}

// emptyStateSized is emptyStateIcon with the icon's extra size class:
// "sm" picks .empty-state .icon.sm (the icon-2xl glyph), "" the
// default.
func emptyStateSized(font render.Font, px float64, icon, title, description, iconClass string) (*widget.Box, *widget.Icon) {
	var extra []string
	if iconClass != "" {
		extra = []string{iconClass}
	}
	col, glyph, desc := templateEmptyState(font, px, icon, extra, title, description)
	desc.SetWrap(true)
	desc.SetMaxWidthChars(32)
	return col, glyph
}

// templateEmptyState is the EmptyState template with its parts handed
// back: the icon for state swaps, the description label for callers
// that tune it (wrap, width cap). iconExtra carries the icon's extra
// classes (the "sm" size); the gaps are the .title/.description
// margin-top rules, and the caller expands the box so it centers.
func templateEmptyState(font render.Font, px float64, icon string, iconExtra []string, title, description string) (*widget.Box, *widget.Icon, *widget.Label) {
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("empty-state")
	glyph := widget.NewThemeIcon(icon, int(px*2))
	glyph.AddClass("icon")
	glyph.AddClass(iconExtra...)
	col.Append(glyph, false)
	titleLbl := widget.NewLabel(font, px*1.1, title, 0)
	titleLbl.AddClass("title")
	col.Append(titleLbl, false)
	desc := widget.NewLabel(font, px*0.9, description, 0)
	desc.AddClass("description")
	col.Append(desc, false)
	return col, glyph, desc
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

// dropdownButton is a flat dropdown button: the class's stylesheet
// rules paint it (the hover and active shades, the ink), so nothing is
// set here.
func dropdownButton(child widget.Widget, class string, onClick func()) *widget.Button {
	b := widget.NewButton(child, 6, 8)
	b.AddClass(class)
	b.OnClick = onClick
	return b
}

// iconTile wraps icon in the styled tile box the stylesheets target:
// the box paints the tile (overlay background, radius, min size) and
// the image paints through its own class (size, ink, margin).
func iconTile(icon *widget.Icon, boxClass, imgClass string) *widget.Box {
	icon.AddClass(imgClass)
	tile := widget.NewBox(widget.Row, 0, 0)
	tile.AddClass(boxClass)
	tile.Append(icon, false)
	return tile
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

// Stack transition timings: a GtkStack's default duration, the hover
// swaps' HOVER_TRANSITION_MS, and the page slides' interaction-duration.
const (
	gtkStackDuration = 200 * time.Millisecond
	hoverTransition  = 150 * time.Millisecond
)

// pageSlide gives a dropdown's page stack its slide between pages at
// the configured interaction-duration (zero, an instant switch, with
// animations off).
func pageSlide(s *widget.Stack, cfg *config.Config) {
	s.SetTransition(widget.StackSlideLeftRight, time.Duration(cfg.Animations.InteractionDurationMs())*time.Millisecond)
}
