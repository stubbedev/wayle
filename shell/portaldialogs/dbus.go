// Package portaldialogs is the shell's com.wayle.PortalDialogs1 host
// (wayle-shell services/portal_dialogs and shell/portal_dialogs): the
// native dialogs behind the portal backend's Access, Account,
// AppChooser, DynamicLauncher and Wallpaper preview, in place of
// xdg-desktop-portal-gtk.
package portaldialogs

import (
	"context"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// D-Bus identity (wayle-ipc portal_dialogs.rs).
const (
	ServiceName = "com.wayle.PortalDialogs1"
	ServicePath = dbus.ObjectPath("/com/wayle/PortalDialogs")
	iface       = "com.wayle.PortalDialogs1"
)

// AccessRequest is one grant/deny prompt.
type AccessRequest struct {
	Title, Subtitle, Body string
	GrantLabel, DenyLabel string
	// Icon is a themed icon name for the permission, "" for none.
	Icon string
}

// Host shows the dialogs; each call blocks until the user answers.
type Host interface {
	// Access replies whether the user granted.
	Access(AccessRequest) bool
	// Account replies whether the user shares their account info.
	Account(reason string) bool
	// ConfirmWallpaper previews a file:// image and replies whether to
	// set it.
	ConfirmWallpaper(uri string) bool
	// ChooseApplication replies the chosen desktop-file id, "" on
	// cancel; choices are candidate ids (none offers every app).
	ChooseApplication(choices []string, contentType, uri string) string
	// ConfirmInstall replies whether to install a dynamic launcher.
	ConfirmInstall(name, iconName string) bool
}

// Daemon serves a Host as com.wayle.PortalDialogs1.
type Daemon struct{ host Host }

// NewDaemon wraps the host for export.
func NewDaemon(host Host) *Daemon { return &Daemon{host: host} }

// Export serves the interface under its name; the returned release
// drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	return dbusx.Serve(conn, dbusx.Service{Name: ServiceName, Path: ServicePath, Interface: iface, Methods: d})
}

// Access is the grant/deny prompt.
func (d *Daemon) Access(title, subtitle, body, grantLabel, denyLabel, icon string) (bool, *dbus.Error) {
	return d.host.Access(AccessRequest{title, subtitle, body, grantLabel, denyLabel, icon}), nil
}

// Account is the account-info consent.
func (d *Daemon) Account(reason string) (bool, *dbus.Error) { return d.host.Account(reason), nil }

// ConfirmWallpaper is the wallpaper preview confirmation.
func (d *Daemon) ConfirmWallpaper(uri string) (bool, *dbus.Error) {
	return d.host.ConfirmWallpaper(uri), nil
}

// ChooseApplication is the app chooser.
func (d *Daemon) ChooseApplication(choices []string, contentType, uri string) (string, *dbus.Error) {
	return d.host.ChooseApplication(choices, contentType, uri), nil
}

// ConfirmInstall is the dynamic-launcher install confirmation.
func (d *Daemon) ConfirmInstall(name, iconName string) (bool, *dbus.Error) {
	return d.host.ConfirmInstall(name, iconName), nil
}

// Client calls the shell's dialogs; a dialog waits on the user, so the
// calls are bounded only by ctx.
type Client struct{ obj dbus.BusObject }

// NewClient addresses the host over conn.
func NewClient(conn *dbus.Conn) *Client { return &Client{obj: conn.Object(ServiceName, ServicePath)} }

func (c *Client) call(ctx context.Context, method string, out any, args ...any) error {
	return c.obj.CallWithContext(ctx, iface+"."+method, 0, args...).Store(out)
}

// Access shows the grant/deny prompt.
func (c *Client) Access(ctx context.Context, r AccessRequest) (granted bool, err error) {
	err = c.call(ctx, "Access", &granted, r.Title, r.Subtitle, r.Body, r.GrantLabel, r.DenyLabel, r.Icon)
	return granted, err
}

// Account asks to share the account info.
func (c *Client) Account(ctx context.Context, reason string) (shared bool, err error) {
	err = c.call(ctx, "Account", &shared, reason)
	return shared, err
}

// ConfirmWallpaper previews uri and asks to set it.
func (c *Client) ConfirmWallpaper(ctx context.Context, uri string) (ok bool, err error) {
	err = c.call(ctx, "ConfirmWallpaper", &ok, uri)
	return ok, err
}

// ChooseApplication picks a handler; "" is a cancel.
func (c *Client) ChooseApplication(ctx context.Context, choices []string, contentType, uri string) (id string, err error) {
	if choices == nil {
		choices = []string{}
	}
	err = c.call(ctx, "ChooseApplication", &id, choices, contentType, uri)
	return id, err
}

// ConfirmInstall asks to install a dynamic launcher.
func (c *Client) ConfirmInstall(ctx context.Context, name, iconName string) (ok bool, err error) {
	err = c.call(ctx, "ConfirmInstall", &ok, name, iconName)
	return ok, err
}
