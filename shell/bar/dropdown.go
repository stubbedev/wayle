package bar

import (
	"fmt"
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
	// openRev is the revealer around each open popover's card.
	openRev map[string]*widget.Revealer
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
	}
}

// attachHost registers the layer window a connector's dropdowns open
// on.
func (r *dropdownRegistry) attachHost(connector string, host app.Host) {
	r.mu.Lock()
	r.hosts[connector] = host
	r.mu.Unlock()
}

// detachHost forgets an unplugged connector's layer and its open
// popover.
func (r *dropdownRegistry) detachHost(connector string) {
	r.mu.Lock()
	delete(r.hosts, connector)
	delete(r.openPop, connector)
	delete(r.openRev, connector)
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
// another re-anchors (the Rust registry's toggle_for).
func (r *dropdownRegistry) open(connector, name string, anchor widget.Widget) error {
	r.mu.Lock()
	// A popover that already went away (click-away, Esc) no longer
	// counts as open: the next click opens instead of toggling off.
	if prev := r.openPop[connector]; prev != nil && !prev.Closed() {
		rev := r.openRev[connector]
		delete(r.openPop, connector)
		delete(r.openRev, connector)
		r.mu.Unlock()
		dismissAnimated(prev, rev, r.cfg.Animations)
		return nil
	}
	host := r.hosts[connector]
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
	// at the global scale).
	if w, h, ok := dropdownDims(name, r.cfg); ok {
		content = newPanelBox(w, h, content)
	}
	cfg := app.PopoverConfig{
		Anchor:  bound,
		Gravity: dropdownGravity(r.cfg.Bar.Location),
		Content: content,
		Serial:  r.app.LastPressSerial(host),
	}
	if closer, ok := content.(dropdownCloser); ok {
		cfg.OnClosed = closer.dropdownClosed
	}
	// The card enters through the dropdown transition (animate_in).
	rev := widget.NewRevealer(content)
	rev.SetGenieEdge(dropdownGenieEdge(r.cfg.Bar.Location))
	cfg.Content = rev
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
