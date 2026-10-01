package bar

import (
	"fmt"
	"sync"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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
}

// dropdownCloser is live dropdown content (a service subscription) that
// releases what it holds when its popover goes away.
type dropdownCloser interface{ dropdownClosed() }

func newDropdownRegistry(application *app.Application, cfg *config.Config, font render.Font, style *barStyle, base ModuleContext) *dropdownRegistry {
	return &dropdownRegistry{
		app:      application,
		cfg:      cfg,
		font:     font,
		style:    style,
		ctx:      base,
		hosts:    make(map[string]app.Host),
		builders: dropdownBuilders(),
		openPop:  make(map[string]*app.Popover),
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
		delete(r.openPop, connector)
		r.mu.Unlock()
		prev.Dismiss()
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
		Content: content,
		Serial:  r.app.LastPressSerial(host),
	}
	if closer, ok := content.(dropdownCloser); ok {
		cfg.OnClosed = closer.dropdownClosed
	}
	pop, err := r.app.OpenPopover(host, cfg)
	if err != nil {
		if cfg.OnClosed != nil {
			cfg.OnClosed()
		}
		return err
	}
	r.mu.Lock()
	r.openPop[connector] = pop
	r.mu.Unlock()
	return nil
}
