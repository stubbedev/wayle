package bar

import (
	"log"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/shellipc"
)

// barSet tracks each output's bar for `wayle panel hide|show|toggle`
// (the Rust shell's BarMap plus the hidden_bars watcher) and config
// reloads. gelm cannot unmap a layer surface, so hiding closes the
// bar's layer and showing builds a fresh one with open.
type barSet struct {
	open  func(*app.Output, config.BarLayout) (barLayer, error)
	order []string
	bars  map[string]*barEntry
}

// barLayer is the part of a bar's layer window the set drives.
type barLayer interface{ Close() }

type barEntry struct {
	output *app.Output
	layout config.BarLayout
	// shows is whether the layout gives the output a bar; hidden is
	// the Shell1 hide state. The layer is open exactly when shows and
	// not hidden.
	shows, hidden bool
	layer         barLayer
}

func newBarSet(open func(*app.Output, config.BarLayout) (barLayer, error)) *barSet {
	return &barSet{open: open, bars: map[string]*barEntry{}}
}

// add registers an output; layer is its open bar, nil when the layout
// gives it none.
func (b *barSet) add(output *app.Output, layout config.BarLayout, layer barLayer) {
	if _, ok := b.bars[output.Name]; !ok {
		b.order = append(b.order, output.Name)
	}
	b.bars[output.Name] = &barEntry{output: output, layout: layout, shows: layer != nil, layer: layer}
}

// connectors names the outputs whose layout gives them a bar, in
// creation order.
func (b *barSet) connectors() []string {
	var out []string
	for _, name := range b.order {
		if b.bars[name].shows {
			out = append(out, name)
		}
	}
	return out
}

// apply closes the bars in hidden and reopens the others. It runs on
// the loop goroutine.
func (b *barSet) apply(hidden map[string]bool) {
	for _, name := range b.order {
		entry := b.bars[name]
		entry.hidden = hidden[name]
		b.sync(name, entry)
	}
}

// reload rebuilds every bar from layoutFor (a config reload): open
// bars close and reopen with the new layout, and outputs the new
// layout shows or hides follow it. The Shell1 hide state survives.
func (b *barSet) reload(layoutFor func(connector string) (config.BarLayout, bool)) {
	for _, name := range b.order {
		entry := b.bars[name]
		if entry.layer != nil {
			entry.layer.Close()
			entry.layer = nil
		}
		entry.layout, entry.shows = layoutFor(name)
		b.sync(name, entry)
	}
}

// sync opens or closes the entry's layer to match shows and hidden.
func (b *barSet) sync(name string, entry *barEntry) {
	want := entry.shows && !entry.hidden
	switch {
	case !want && entry.layer != nil:
		entry.layer.Close()
		entry.layer = nil
	case want && entry.layer == nil:
		layer, err := b.open(entry.output, entry.layout)
		if err != nil {
			log.Printf("bar %s: show: %v", name, err)
			return
		}
		entry.layer = layer
	}
}

// serveShellIPC takes the application id (its quit action ends the
// loop) and serves com.wayle.Shell1 over bars, with lock as its Lock hook. A shell already owning
// the id is logged: the single-instance check runs before RunWith.
func serveShellIPC(application *app.Application, bars *barSet, lock func() bool) func() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("shell ipc: session bus: %v", err)
		return func() {}
	}
	var releases []func()
	quit := func() { application.Invoke(application.Quit) }
	if release, err := shellipc.ServeApplication(conn, quit); err == nil {
		releases = append(releases, release)
	} else {
		log.Printf("shell ipc: application id: %v", err)
	}
	state := shellipc.NewState(func(hidden map[string]bool) {
		application.Invoke(func() { bars.apply(hidden) })
	})
	state.SetConnectors(bars.connectors())
	if release, err := shellipc.Serve(conn, state, shellipc.Hooks{
		Lock: lock,
		// The VPN callback goes straight to the native sign-in, whose
		// waiting browser sign-ins are process-wide, as the Rust
		// daemon's are.
		VPNSSOCallback: network.NativeSignIn.DeliverSSOCallback,
	}); err == nil {
		releases = append(releases, release)
	} else {
		log.Printf("shell ipc: %v", err)
	}
	return func() {
		for _, release := range releases {
			release()
		}
		_ = conn.Close()
	}
}
