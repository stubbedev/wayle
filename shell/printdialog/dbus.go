// Package printdialog is the shell's com.wayle.Print1 host (wayle-shell
// services/print and shell/print): the print dialog and spooler behind
// the portal backend's Print.
package printdialog

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// D-Bus identity (wayle-ipc print.rs).
const (
	ServiceName = "com.wayle.Print1"
	ServicePath = dbus.ObjectPath("/com/wayle/Print")
	iface       = "com.wayle.Print1"
)

// Setting is one flat print-setting key/value pair.
type Setting struct{ Key, Value string }

// Host shows the dialog and spools the jobs.
type Host interface {
	// Prepare lets the user pick a printer and settings for title and
	// stashes them under the returned token; granted is false on a
	// cancel.
	Prepare(title string) (granted bool, settings []Setting, token uint32)
	// Print spools document (a PDF the host takes ownership of) to the
	// printer prepared under token, and reports whether it was sent.
	Print(title string, document *os.File, token uint32) bool
}

// Daemon serves a Host as com.wayle.Print1.
type Daemon struct{ host Host }

// NewDaemon wraps the host for export.
func NewDaemon(host Host) *Daemon { return &Daemon{host: host} }

// Export serves the interface under its name; the returned release
// drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	return dbusx.Serve(conn, dbusx.Service{Name: ServiceName, Path: ServicePath, Interface: iface, Methods: d})
}

// Prepare shows the dialog.
func (d *Daemon) Prepare(title string) (bool, []Setting, uint32, *dbus.Error) {
	granted, settings, token := d.host.Prepare(title)
	if settings == nil {
		settings = []Setting{}
	}
	return granted, settings, token, nil
}

// Print spools document.
func (d *Daemon) Print(title string, document dbus.UnixFD, token uint32) (bool, *dbus.Error) {
	return d.host.Print(title, os.NewFile(uintptr(document), "document"), token), nil
}

// Client calls the shell's print host; Prepare waits on the user.
type Client struct{ obj dbus.BusObject }

// NewClient addresses the host over conn.
func NewClient(conn *dbus.Conn) *Client { return &Client{obj: conn.Object(ServiceName, ServicePath)} }

// Prepare shows the dialog.
func (c *Client) Prepare(ctx context.Context, title string) (granted bool, settings []Setting, token uint32, err error) {
	err = c.obj.CallWithContext(ctx, iface+".Prepare", 0, title).Store(&granted, &settings, &token)
	return granted, settings, token, err
}

// Print spools document, which stays the caller's to close.
func (c *Client) Print(ctx context.Context, title string, document dbus.UnixFD, token uint32) (sent bool, err error) {
	err = c.obj.CallWithContext(ctx, iface+".Print", 0, title, document, token).Store(&sent)
	return sent, err
}
