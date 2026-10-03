package bar

import (
	"fmt"
	"math"
	"sync"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/reveal"
)

// dropdownRegistry maps the `dropdown:<name>` actions to content
// builders and owns the open popover per output host. RunWith creates
// one; the builders live next to their modules (calendar, audio, ...)
// exactly as the Rust dropdowns do.
type dropdownRegistry struct {
	app      *app.Application
	cfg      *config.Config
	font     render.Font
	style    *barStyle
	ctx      ModuleContext
	hosts    map[string]app.Host
	builders map[string]func(ctx ModuleContext) widget.Widget

	mu      sync.Mutex
	openPop map[string]*app.Popover
	// openRev is the revealer around each open popover's card; openName
	// is the dropdown it shows, so a second module's click re-anchors
	// instead of toggling off.
	openRev  map[string]*widget.Revealer
	openName map[string]string
	// monH is each connector's monitor height in logical pixels, the
	// popover ceiling's input (GTK keeps a popover on its output).
	monH map[string]int
}

// dropdownCloser is live dropdown content (a service subscription) that
// releases what it holds when its popover goes away.
type dropdownCloser interface{ dropdownClosed() }

// popoverHandle is what open dropdown content may do to its popover:
// close it (GTK's popdown) and move its keyboard focus (grab_focus).
// *app.Popover implements it.
type popoverHandle interface {
	Dismiss()
	SetFocus(w widget.Widget)
}

// dropdownAttacher is content that acts on its own popover; the
// registry hands it the handle once the popover is up.
type dropdownAttacher interface{ attachPopover(h popoverHandle) }

// popoverHook is the dropdownAttacher half content embeds. popdown is a
// no-op until a popover is open; a focus asked for while the content is
// still being built (a form shown before the popover exists) is kept and
// applied once it attaches.
type popoverHook struct {
	h       popoverHandle
	pending widget.Widget
}

func (p *popoverHook) attachPopover(h popoverHandle) {
	p.h = h
	if p.pending != nil {
		h.SetFocus(p.pending)
		p.pending = nil
	}
}

// popdown closes the hosting popover.
func (p *popoverHook) popdown() {
	if p.h != nil {
		p.h.Dismiss()
	}
}

// focus moves keyboard focus to w inside the hosting popover, or once
// it opens.
func (p *popoverHook) focus(w widget.Widget) {
	if p.h == nil {
		p.pending = w
		return
	}
	p.h.SetFocus(w)
}

func newDropdownRegistry(application *app.Application, cfg *config.Config, font render.Font, style *barStyle, base ModuleContext) *dropdownRegistry {
	// Dropdown content paints its own colors (no stylesheet reaches a
	// popover), so builders get the full style: the module style the
	// bar hands its modules leaves fg to the stylesheet (zero), which
	// would paint every default-colored dropdown label transparent.
	base.Style = style
	return &dropdownRegistry{
		app:      application,
		cfg:      cfg,
		font:     font,
		style:    style,
		ctx:      base,
		hosts:    make(map[string]app.Host),
		builders: dropdownBuilders(),
		openPop:  make(map[string]*app.Popover),
		openRev:  make(map[string]*widget.Revealer),
		openName: make(map[string]string),
		monH:     make(map[string]int),
	}
}

// attachHost registers the layer window a connector's dropdowns open
// on, with the monitor's logical height for the popover ceiling.
func (r *dropdownRegistry) attachHost(connector string, host app.Host, monH int) {
	r.mu.Lock()
	r.hosts[connector] = host
	r.monH[connector] = monH
	r.mu.Unlock()
}

// detachHost forgets an unplugged connector's layer and its open
// popover.
func (r *dropdownRegistry) detachHost(connector string) {
	r.mu.Lock()
	delete(r.hosts, connector)
	delete(r.openPop, connector)
	delete(r.openRev, connector)
	delete(r.openName, connector)
	delete(r.monH, connector)
	r.mu.Unlock()
}

// Names lists the registered dropdowns (tests).
func (r *dropdownRegistry) Names() []string {
	names := make([]string, 0, len(r.builders))
	for name := range r.builders {
		names = append(names, name)
	}
	return names
}

// open shows one dropdown anchored to the module's root widget on the
// connector's host. Re-clicking the same module dismisses; opening
// another while one is up dismisses it and opens the new (the Rust
// registry's reparent_and_show), so the bar never shows two.
func (r *dropdownRegistry) open(connector, name string, anchor widget.Widget) error {
	r.mu.Lock()
	host := r.hosts[connector]
	monH := r.monH[connector]
	// A popover that already went away (click-away, Esc) no longer
	// counts as open: the next click opens instead of toggling off.
	if prev := r.openPop[connector]; prev != nil && !prev.Closed() {
		same := r.openName[connector] == name
		rev := r.openRev[connector]
		delete(r.openPop, connector)
		delete(r.openRev, connector)
		delete(r.openName, connector)
		r.mu.Unlock()
		dismissAnimated(prev, rev, r.cfg.Animations)
		if same {
			return nil
		}
		// The exit plays before the next popover opens.
		r.mu.Lock()
	}
	build, ok := r.builders[name]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("dropdown %q is not registered", name)
	}
	if host == nil || r.app == nil {
		return fmt.Errorf("dropdown %q has no host yet", name)
	}
	bound, ok := anchor.(widget.Boundser)
	if !ok {
		return fmt.Errorf("dropdown %q anchor cannot be measured", name)
	}
	content := build(r.ctx)
	if content == nil {
		return fmt.Errorf("dropdown %q has no content", name)
	}
	// The panel takes its [dropdowns.<name>] size (or the built-in base
	// at the global scale), its content clamped to the monitor less the
	// Rust registry's scrolled-content margin, the popover surface to
	// the monitor less its own.
	if w, h, ok := dropdownDims(name, r.cfg); ok {
		panel := newPanelBox(w, h, content)
		if monH > 0 {
			panel.maxH = monH - 180
		}
		content = panel
	}
	cfg := app.PopoverConfig{
		Anchor:  bound,
		Gravity: dropdownGravity(r.cfg.Bar.Location),
		Content: content,
		Serial:  r.app.LastPressSerial(host),
	}
	if monH > 0 {
		cfg.MaxHeight = monH - 100
	}
	if closer, ok := content.(dropdownCloser); ok {
		cfg.OnClosed = closer.dropdownClosed
	}
	// The card enters through the dropdown transition (animate_in).
	rev := widget.NewRevealer(content)
	rev.SetGenieEdge(dropdownGenieEdge(r.cfg.Bar.Location))
	// The popover carries the GTK class chain the stylesheet styles
	// through: popover.dropdown > contents .dropdown gets the surface,
	// and the shadow/position classes pick the shadow's direction. The
	// popover is its own tree, so the theme sheet attaches here — the
	// panels paint from the same CSS the Rust shell loads.
	card := popoverCard(rev, dropdownGravity(r.cfg.Bar.Location), r.cfg.Bar.DropdownShadow)
	// The Rust registry's surface margins: a narrow gap toward the bar,
	// the wide one everywhere else, scaled.
	framed := withDropdownMargins(card, r.cfg)
	cfg.Content = framed
	if r.ctx.Theme != nil {
		r.ctx.Theme.Attach(card)
	}
	pop, err := r.app.OpenPopover(host, cfg)
	if err != nil {
		if cfg.OnClosed != nil {
			cfg.OnClosed()
		}
		return err
	}
	if d, ok := content.(dropdownAttacher); ok {
		d.attachPopover(pop)
	}
	reveal.Show(rev, r.cfg.Animations, config.AnimDropdown)
	r.mu.Lock()
	r.openPop[connector] = pop
	r.openRev[connector] = rev
	r.openName[connector] = name
	r.mu.Unlock()
	return nil
}

// dismissAnimated is animate_out: the card plays its exit, then the
// popover goes. Only this programmatic close animates; a click-away or
// Esc closes at once, the compositor's grab having no exit hook.
func dismissAnimated(pop interface{ Dismiss() }, rev *widget.Revealer, anims config.AnimationsConfig) {
	if rev == nil {
		pop.Dismiss()
		return
	}
	reveal.Hide(rev, anims, config.AnimDropdown, pop.Dismiss)
}

// dropdownGenieEdge is the edge a genie collapses toward: the bar's.
func dropdownGenieEdge(location config.Location) widget.Edge {
	switch location {
	case config.LocationBottom:
		return widget.EdgeBottom
	case config.LocationLeft:
		return widget.EdgeLeft
	case config.LocationRight:
		return widget.EdgeRight
	}
	return widget.EdgeTop
}

// setConfig hands dropdowns opened from now on a new snapshot, for a
// reload that leaves the bars standing.
func (r *dropdownRegistry) setConfig(cfg *config.Config) {
	r.cfg = cfg
	r.ctx.Config = cfg
}

// dropdownMargins is the Rust registry's DropdownMargins: a narrow gap
// toward the bar's edge, the wide one everywhere else, rounded to
// whole pixels at the bar scale.
type dropdownMargins struct{ top, bottom, start, end int }

const (
	marginsGapRem     = 0.275
	marginsContentRem = 1.0
	marginsRemPx      = 16.0
)

func newDropdownMargins(scale float64, location config.Location) dropdownMargins {
	gap := roundMarginRem(marginsGapRem, scale)
	content := roundMarginRem(marginsContentRem, scale)
	m := dropdownMargins{content, content, content, content}
	switch location {
	case config.LocationTop:
		m.top = gap
	case config.LocationBottom:
		m.bottom = gap
	case config.LocationLeft:
		m.start = gap
	case config.LocationRight:
		m.end = gap
	}
	return m
}

func roundMarginRem(rem, scale float64) int {
	return int(math.Round(rem * marginsRemPx * scale))
}

// withDropdownMargins wraps the popover card in the margins box: the
// Rust registry sets them on the popover child at every show, so the
// card floats inside the surface away from its edges.
func withDropdownMargins(card widget.Widget, cfg *config.Config) widget.Widget {
	m := newDropdownMargins(float64(cfg.Styling.Scale), cfg.Bar.Location)
	box := widget.NewBox(widget.Column, 0, 0)
	box.SetPadding(render.Insets{Top: m.top, Bottom: m.bottom, Left: m.start, Right: m.end})
	box.Append(card, true)
	return box
}

// dropdownGravity is detect_popover_position: dropdowns open away from
// the bar's screen edge.
func dropdownGravity(location config.Location) app.Gravity {
	switch location {
	case config.LocationBottom:
		return app.GravityTop
	case config.LocationLeft:
		return app.GravityRight
	case config.LocationRight:
		return app.GravityLeft
	}
	return app.GravityBottom
}
