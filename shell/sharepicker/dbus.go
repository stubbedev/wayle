package sharepicker

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// D-Bus identity (wayle-ipc's share_picker proxy).
const (
	ServiceName = "com.wayle.SharePicker1"
	ServicePath = "/com/wayle/SharePicker"
	iface       = "com.wayle.SharePicker1"
)

// picker is what the daemon serves: the Picker, or a fake in tests.
type picker interface {
	Pick(windowList string, allowToken, multiple bool) string
}

// Daemon exports a Picker as com.wayle.SharePicker1.
type Daemon struct {
	picker picker
}

// NewDaemon wraps the picker for export.
func NewDaemon(p *Picker) *Daemon { return &Daemon{picker: p} }

// Export exports the object and claims the well-known name; the
// returned release drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	if err := conn.Export(d, ServicePath, iface); err != nil {
		return nil, fmt.Errorf("share picker: export: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("share picker: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("share picker: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// Pick shows the picker and blocks until the user selects or cancels,
// replying the XDPH selection suffix (printed after `[SELECTION]`), or
// an empty string on cancel.
func (d *Daemon) Pick(windowList string, allowToken, multiple bool) (string, *dbus.Error) {
	return d.picker.Pick(windowList, allowToken, multiple), nil
}

// Client calls the daemon: the `wayle portal share-picker` stub XDPH
// execs, and the portal backend's ScreenCast source selection.
type Client struct {
	obj dbus.BusObject
}

// NewClient addresses the daemon over conn.
func NewClient(conn *dbus.Conn) *Client {
	return &Client{obj: conn.Object(ServiceName, ServicePath)}
}

// Pick asks the shell to show the picker; see Daemon.Pick.
func (c *Client) Pick(ctx context.Context, windowList string, allowToken, multiple bool) (string, error) {
	var selection string
	err := c.obj.CallWithContext(ctx, iface+".Pick", 0, windowList, allowToken, multiple).Store(&selection)
	return selection, err
}
