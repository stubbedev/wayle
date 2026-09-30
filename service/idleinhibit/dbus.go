package idleinhibit

import (
	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Daemon is the com.wayle.IdleInhibit1 object
// (wayle-shell-core/src/services/idle_inhibit/dbus.rs), which the
// `wayle idle` CLI drives from another process.
type Daemon struct {
	state *State
}

// NewDaemon wraps the state for export.
func NewDaemon(state *State) *Daemon { return &Daemon{state: state} }

// Export serves the interface under its well-known name; the returned
// release drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	return dbusx.Serve(conn, dbusx.Service{
		Name:      ServiceName,
		Path:      ServicePath,
		Interface: ServiceName,
		Methods:   d,
		Properties: dbusx.Getters{
			"Active":     func() any { return d.state.Active() },
			"Duration":   func() any { return d.state.Duration() },
			"Remaining":  func() any { return uint32(max(d.state.Remaining(), 0)) },
			"Indefinite": func() any { return d.state.Duration() == 0 },
		},
	})
}

// Enable turns inhibition on, indefinite or timed.
func (d *Daemon) Enable(indefinite bool) *dbus.Error {
	d.state.Enable(indefinite)
	return nil
}

// Disable turns inhibition off.
func (d *Daemon) Disable() *dbus.Error {
	d.state.Disable()
	return nil
}

// timerGuard refuses timer edits while inactive or indefinite.
func (d *Daemon) timerGuard(verb string) *dbus.Error {
	if !d.state.Active() {
		return dbusx.Failed("idle inhibit is not active")
	}
	if d.state.Duration() == 0 {
		return dbusx.Failed("cannot " + verb + " timer in indefinite mode")
	}
	return nil
}

// AdjustRemaining shifts the time left by minutes.
func (d *Daemon) AdjustRemaining(deltaMinutes int32) *dbus.Error {
	if err := d.timerGuard("adjust"); err != nil {
		return err
	}
	d.state.AdjustRemaining(deltaMinutes)
	return nil
}

// SetRemaining replaces the time left.
func (d *Daemon) SetRemaining(minutes uint32) *dbus.Error {
	if err := d.timerGuard("set"); err != nil {
		return err
	}
	d.state.SetRemaining(minutes)
	return nil
}

// SetDuration stores the duration in minutes.
func (d *Daemon) SetDuration(minutes uint32) *dbus.Error {
	d.state.SetDuration(minutes)
	return nil
}

// AdjustDuration shifts the stored duration.
func (d *Daemon) AdjustDuration(deltaMinutes int32) *dbus.Error {
	d.state.AdjustDuration(deltaMinutes)
	return nil
}
