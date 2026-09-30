package screenshot

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// D-Bus identity (wayle-ipc's screenshot proxy).
const (
	ServiceName = "com.wayle.Screenshot1"
	ServicePath = "/com/wayle/Screenshot"
	iface       = "com.wayle.Screenshot1"
)

// capturer is what the daemon serves: the Host, or a fake in tests.
type capturer interface {
	Capture(mode, target string) (string, error)
	PickColor() (r, g, b float64, err error)
}

// Daemon exports a Host as com.wayle.Screenshot1.
type Daemon struct {
	host capturer
}

// NewDaemon wraps the host for export.
func NewDaemon(h *Host) *Daemon { return &Daemon{host: h} }

// Export exports the object and claims the well-known name; the
// returned release drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	if err := conn.Export(d, ServicePath, iface); err != nil {
		return nil, fmt.Errorf("screenshot: export: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("screenshot: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("screenshot: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// Capture captures a screenshot and blocks until it is saved or
// cancelled. mode is region, output, screen, or window; target is an
// output connector name (output mode) or empty. Replies the saved PNG
// path, empty when a region selection was cancelled.
func (d *Daemon) Capture(mode, target string) (string, *dbus.Error) {
	path, err := d.host.Capture(mode, target)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return path, nil
}

// PickColor picks one screen color interactively, replying sRGB
// (r, g, b) in [0, 1]; cancelling is an error.
func (d *Daemon) PickColor() (float64, float64, float64, *dbus.Error) {
	r, g, b, err := d.host.PickColor()
	if err != nil {
		return 0, 0, 0, dbus.MakeFailedError(err)
	}
	return r, g, b, nil
}

// Client calls the daemon: the `wayle screenshot` CLI, and the portal
// backend's Screenshot and PickColor.
type Client struct {
	obj dbus.BusObject
}

// NewClient addresses the daemon over conn.
func NewClient(conn *dbus.Conn) *Client {
	return &Client{obj: conn.Object(ServiceName, ServicePath)}
}

// Capture asks the shell for a screenshot; see Daemon.Capture.
func (c *Client) Capture(ctx context.Context, mode, target string) (string, error) {
	var path string
	err := c.obj.CallWithContext(ctx, iface+".Capture", 0, mode, target).Store(&path)
	return path, err
}

// PickColor asks the shell for an interactive color pick.
func (c *Client) PickColor(ctx context.Context) (r, g, b float64, err error) {
	err = c.obj.CallWithContext(ctx, iface+".PickColor", 0).Store(&r, &g, &b)
	return r, g, b, err
}
