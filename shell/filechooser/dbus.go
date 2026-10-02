// Package filechooser is the shell's com.wayle.FileChooser1 host
// (wayle-shell services/file_chooser and shell/file_chooser): the file
// dialog behind the portal backend's FileChooser.
package filechooser

import (
	"context"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// D-Bus identity (wayle-ipc file_chooser.rs).
const (
	ServiceName = "com.wayle.FileChooser1"
	ServicePath = dbus.ObjectPath("/com/wayle/FileChooser")
	iface       = "com.wayle.FileChooser1"
)

// Rule kinds of a Filter.
const (
	RuleGlob uint32 = 0
	RuleMIME uint32 = 1
)

// Rule is one (kind, value) of a filter: a glob or a MIME type.
type Rule struct {
	Kind  uint32
	Value string
}

// Filter is a named list of rules, the portal's (sa(us)).
type Filter struct {
	Name  string
	Rules []Rule
}

// OpenRequest is one open dialog.
type OpenRequest struct {
	Title               string
	Multiple, Directory bool
	Filters             []Filter
	// CurrentFolder seeds the starting directory ("" is the default).
	CurrentFolder string
}

// SaveRequest is one save dialog.
type SaveRequest struct {
	Title         string
	CurrentName   string
	Filters       []Filter
	CurrentFolder string
}

// Host shows the dialogs; each blocks until the user answers and
// replies the chosen file:// URIs, none on a cancel.
type Host interface {
	Open(OpenRequest) []string
	Save(SaveRequest) []string
}

// Daemon serves a Host as com.wayle.FileChooser1.
type Daemon struct{ host Host }

// NewDaemon wraps the host for export.
func NewDaemon(host Host) *Daemon { return &Daemon{host: host} }

// Export serves the interface under its name; the returned release
// drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	return dbusx.Serve(conn, dbusx.Service{Name: ServiceName, Path: ServicePath, Interface: iface, Methods: d})
}

// OpenFile opens existing files or a directory.
func (d *Daemon) OpenFile(title string, multiple, directory bool, filters []Filter, currentFolder string) ([]string, *dbus.Error) {
	return nonNil(d.host.Open(OpenRequest{title, multiple, directory, filters, currentFolder})), nil
}

// SaveFile chooses a save destination.
func (d *Daemon) SaveFile(title, currentName string, filters []Filter, currentFolder string) ([]string, *dbus.Error) {
	return nonNil(d.host.Save(SaveRequest{title, currentName, filters, currentFolder})), nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Client calls the shell's dialog; it waits on the user, so it is
// bounded only by ctx.
type Client struct{ obj dbus.BusObject }

// NewClient addresses the host over conn.
func NewClient(conn *dbus.Conn) *Client { return &Client{obj: conn.Object(ServiceName, ServicePath)} }

// Open shows the open dialog.
func (c *Client) Open(ctx context.Context, r OpenRequest) (uris []string, err error) {
	err = c.obj.CallWithContext(ctx, iface+".OpenFile", 0, r.Title, r.Multiple, r.Directory, nonNil(r.Filters), r.CurrentFolder).Store(&uris)
	return uris, err
}

// Save shows the save dialog.
func (c *Client) Save(ctx context.Context, r SaveRequest) (uris []string, err error) {
	err = c.obj.CallWithContext(ctx, iface+".SaveFile", 0, r.Title, r.CurrentName, nonNil(r.Filters), r.CurrentFolder).Store(&uris)
	return uris, err
}
