package recorder

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// callCtx is the context type the CLI calls take.
type callCtx = context.Context

// D-Bus identity (the Rust RecorderService's registration).
const (
	ServiceName = "com.wayle.Recorder1"
	ServicePath = "/com/wayle/Recorder"
)

// Daemon serves the recorder state machine on the session bus.
type Daemon struct {
	state *State
}

// NewDaemon wraps the state for export.
func NewDaemon(state *State) *Daemon { return &Daemon{state: state} }

// Export requests the well-known name and exports the object; the
// returned release function drops both.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	if err := conn.Export(d, ServicePath, ServiceName); err != nil {
		return nil, fmt.Errorf("recorder: export: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("recorder: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("recorder: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// Toggle starts or stops.
func (d *Daemon) Toggle() (string, error) {
	d.state.Toggle()
	return d.state.Snapshot().Status.String(), nil
}

// Start begins a recording.
func (d *Daemon) Start() (string, error) {
	d.state.Start()
	return d.state.Snapshot().Status.String(), nil
}

// Stop finalizes the recording.
func (d *Daemon) Stop() (string, error) {
	d.state.Stop()
	return d.state.Snapshot().Status.String(), nil
}

// SetPaused pauses or resumes.
func (d *Daemon) SetPaused(paused bool) (bool, error) {
	d.state.SetPaused(paused)
	return d.state.Snapshot().Paused, nil
}

// Status snapshots the state.
func (d *Daemon) Status() (Change, error) { return d.state.Snapshot(), nil }

// Elapsed reports the seconds recorded.
func (d *Daemon) Elapsed() (uint32, error) { return d.state.Snapshot().ElapsedSecs, nil }

// Paused reports the pause flag.
func (d *Daemon) Paused() (bool, error) { return d.state.Snapshot().Paused, nil }

// Active reports whether a recording is live.
func (d *Daemon) Active() (bool, error) { return d.state.Snapshot().Active, nil }

// Client drives the daemon from the CLI.
type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Connect dials the session bus.
func Connect() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("recorder: session bus: %w", err)
	}
	return &Client{conn: conn, obj: conn.Object(ServiceName, ServicePath)}, nil
}

// Close drops the bus connection.
func (c *Client) Close() error { return c.conn.Close() }

// Toggle flips the recording.
func (c *Client) Toggle(ctx callCtx) (string, error) {
	var status string
	err := c.obj.CallWithContext(ctx, ServiceName+".Toggle", 0).Store(&status)
	return status, err
}

// Start begins a recording.
func (c *Client) Start(ctx callCtx) (string, error) {
	var status string
	err := c.obj.CallWithContext(ctx, ServiceName+".Start", 0).Store(&status)
	return status, err
}

// Stop finalizes the recording.
func (c *Client) Stop(ctx callCtx) (string, error) {
	var status string
	err := c.obj.CallWithContext(ctx, ServiceName+".Stop", 0).Store(&status)
	return status, err
}

// SetPaused pauses or resumes.
func (c *Client) SetPaused(ctx callCtx, paused bool) (bool, error) {
	var on bool
	err := c.obj.CallWithContext(ctx, ServiceName+".SetPaused", 0, paused).Store(&on)
	return on, err
}

// Status reads the snapshot.
func (c *Client) Status(ctx callCtx) (Change, error) {
	var snap Change
	err := c.obj.CallWithContext(ctx, ServiceName+".Status", 0).Store(&snap)
	return snap, err
}
