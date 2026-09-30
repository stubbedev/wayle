package bar

import (
	"log"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/shellipc"
)

// barSet tracks each output's bar for `wayle panel hide|show|toggle`
// (the Rust shell's BarMap plus the hidden_bars watcher). gelm cannot
// unmap a layer surface, so hiding closes the bar's layer and showing
// builds a fresh one with open.
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
	layer  barLayer // nil while hidden
}

func newBarSet(open func(*app.Output, config.BarLayout) (barLayer, error)) *barSet {
	return &barSet{open: open, bars: map[string]*barEntry{}}
}

func (b *barSet) add(output *app.Output, layout config.BarLayout, layer barLayer) {
	if _, ok := b.bars[output.Name]; !ok {
		b.order = append(b.order, output.Name)
	}
	b.bars[output.Name] = &barEntry{output: output, layout: layout, layer: layer}
}

// connectors names the outputs with a bar, in creation order.
func (b *barSet) connectors() []string { return append([]string(nil), b.order...) }

// apply closes the bars in hidden and reopens the others. It runs on
// the loop goroutine.
func (b *barSet) apply(hidden map[string]bool) {
	for _, name := range b.order {
		entry := b.bars[name]
		switch {
		case hidden[name] && entry.layer != nil:
			entry.layer.Close()
			entry.layer = nil
		case !hidden[name] && entry.layer == nil:
			layer, err := b.open(entry.output, entry.layout)
			if err != nil {
				log.Printf("bar %s: show: %v", name, err)
				continue
			}
			entry.layer = layer
		}
	}
}

// serveShellIPC takes the application id (its quit action ends the
// loop) and serves com.wayle.Shell1 over bars. A shell already owning
// the id is logged: the single-instance check runs before RunWith.
func serveShellIPC(application *app.Application, bars *barSet) func() {
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
	if release, err := shellipc.Serve(conn, state, shellipc.Hooks{}); err == nil {
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
