package idleinhibit

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// Daemon serves the state on the session bus so the `wayle idle` CLI
// can drive it from another process.
type Daemon struct {
	state *State
}

// NewDaemon wraps the state for export.
func NewDaemon(state *State) *Daemon { return &Daemon{state: state} }

// Export requests the well-known name and exports the object; the
// returned release function drops both.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	if err := conn.Export(d, ServicePath, ServiceName); err != nil {
		return nil, fmt.Errorf("idleinhibit: export: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("idleinhibit: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("idleinhibit: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// Enable enables inhibition, indefinite or timed.
func (d *Daemon) Enable(indefinite bool) (bool, error) {
	d.state.Enable(indefinite)
	return d.state.Active(), nil
}

// Disable turns inhibition off.
func (d *Daemon) Disable() (bool, error) {
	d.state.Disable()
	return d.state.Active(), nil
}

// AdjustRemaining shifts the seconds left by minutes.
func (d *Daemon) AdjustRemaining(deltaMinutes int32) (int32, error) {
	d.state.AdjustRemaining(deltaMinutes)
	return int32(d.state.Remaining()), nil
}

// SetRemaining replaces the seconds left.
func (d *Daemon) SetRemaining(minutes uint32) (int32, error) {
	d.state.SetRemaining(minutes)
	return int32(d.state.Remaining()), nil
}

// SetDuration stores the duration in minutes.
func (d *Daemon) SetDuration(minutes uint32) (uint32, error) {
	d.state.SetDuration(minutes)
	return d.state.Duration(), nil
}

// AdjustDuration shifts the stored duration.
func (d *Daemon) AdjustDuration(deltaMinutes int32) (uint32, error) {
	d.state.AdjustDuration(deltaMinutes)
	return d.state.Duration(), nil
}

// Active reports the active flag.
func (d *Daemon) Active() (bool, error) { return d.state.Active(), nil }

// Duration reports the stored minutes.
func (d *Daemon) Duration() (uint32, error) { return d.state.Duration(), nil }

// Remaining reports the seconds left.
func (d *Daemon) Remaining() (int32, error) { return int32(d.state.Remaining()), nil }

// Indefinite reports the indefinite mode.
func (d *Daemon) Indefinite() (bool, error) { return d.state.Indefinite(), nil }

// DBus is the CLI-side proxy over the session bus.
type DBus struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Connect dials the session bus.
func Connect() (*DBus, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("idleinhibit: session bus: %w", err)
	}
	return &DBus{conn: conn, obj: conn.Object(ServiceName, ServicePath)}, nil
}

// Close drops the bus connection.
func (c *DBus) Close() error { return c.conn.Close() }

// Enable turns inhibition on.
func (c *DBus) Enable(ctx context.Context, indefinite bool) error {
	return c.obj.CallWithContext(ctx, ServiceName+".Enable", 0, indefinite).Err
}

// Disable turns inhibition off.
func (c *DBus) Disable(ctx context.Context) error {
	return c.obj.CallWithContext(ctx, ServiceName+".Disable", 0).Err
}

// AdjustRemaining shifts the seconds left.
func (c *DBus) AdjustRemaining(ctx context.Context, deltaMinutes int32) error {
	return c.obj.CallWithContext(ctx, ServiceName+".AdjustRemaining", 0, deltaMinutes).Err
}

// SetRemaining replaces the seconds left.
func (c *DBus) SetRemaining(ctx context.Context, minutes uint32) error {
	return c.obj.CallWithContext(ctx, ServiceName+".SetRemaining", 0, minutes).Err
}

// AdjustDuration shifts the stored duration.
func (c *DBus) AdjustDuration(ctx context.Context, deltaMinutes int32) error {
	return c.obj.CallWithContext(ctx, ServiceName+".AdjustDuration", 0, deltaMinutes).Err
}

// SetDuration stores the duration.
func (c *DBus) SetDuration(ctx context.Context, minutes uint32) error {
	return c.obj.CallWithContext(ctx, ServiceName+".SetDuration", 0, minutes).Err
}

// Status reads the daemon's snapshot.
func (c *DBus) Status(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	if err := c.obj.CallWithContext(ctx, ServiceName+".Active", 0).Store(&snap.Active); err != nil {
		return snap, err
	}
	if err := c.obj.CallWithContext(ctx, ServiceName+".Duration", 0).Store(&snap.DurationMins); err != nil {
		return snap, err
	}
	var remaining int32
	if err := c.obj.CallWithContext(ctx, ServiceName+".Remaining", 0).Store(&remaining); err != nil {
		return snap, err
	}
	snap.RemainingS = int(remaining)
	return snap, nil
}
