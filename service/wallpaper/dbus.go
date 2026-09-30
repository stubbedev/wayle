package wallpaper

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// D-Bus identity (dbus/mod.rs, dbus.md).
const (
	ServiceName = "com.wayle.Wallpaper1"
	ServicePath = dbus.ObjectPath("/com/wayle/Wallpaper")
	Interface   = "com.wayle.Wallpaper1"
)

const (
	errFailed      = "org.freedesktop.DBus.Error.Failed"
	errInvalidArgs = "org.freedesktop.DBus.Error.InvalidArgs"
	propsInterface = "org.freedesktop.DBus.Properties"
)

func failed(err error) *dbus.Error {
	return &dbus.Error{Name: errFailed, Body: []any{err.Error()}}
}

func invalidArgs(err error) *dbus.Error {
	return &dbus.Error{Name: errInvalidArgs, Body: []any{err.Error()}}
}

// Daemon serves a Service as com.wayle.Wallpaper1 (dbus/server.rs). An
// empty monitor argument targets every monitor.
type Daemon struct {
	svc *Service
}

// Export exports the object, its read-only properties, and
// introspection, requests the well-known name, and forwards every
// finished extraction as the ColorsExtracted signal. The returned
// function releases the name and stops the forwarding.
func Export(conn *dbus.Conn, svc *Service) (func(), error) {
	d := &Daemon{svc: svc}
	if err := conn.Export(d, ServicePath, Interface); err != nil {
		return nil, fmt.Errorf("wallpaper: export: %w", err)
	}
	if err := conn.Export(properties{svc}, ServicePath, propsInterface); err != nil {
		return nil, fmt.Errorf("wallpaper: export properties: %w", err)
	}
	node := &introspect.Node{
		Name: string(ServicePath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: propsInterface, Methods: introspect.Methods(properties{})},
			{
				Name:    Interface,
				Methods: introspect.Methods(d),
				Properties: []introspect.Property{
					{Name: "IsCycling", Type: "b", Access: "read"},
					{Name: "ThemingMonitor", Type: "s", Access: "read"},
				},
				Signals: []introspect.Signal{{Name: "ColorsExtracted"}},
			},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), ServicePath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("wallpaper: export introspection: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("wallpaper: cannot acquire D-Bus name '%s': %w", ServiceName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("wallpaper: cannot acquire D-Bus name '%s': already owned", ServiceName)
	}
	extracted, unsubscribe := svc.Extracted()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-extracted:
				_ = conn.Emit(ServicePath, Interface+".ColorsExtracted")
			}
		}
	}()
	return func() {
		close(done)
		unsubscribe()
		_, _ = conn.ReleaseName(ServiceName)
	}, nil
}

// SetWallpaper sets path on a monitor, or all monitors for "".
func (d *Daemon) SetWallpaper(path, monitor string) *dbus.Error {
	if err := d.svc.SetWallpaper(path, monitor); err != nil {
		return failed(err)
	}
	return nil
}

// SetFitMode sets the fit mode on a monitor, or all monitors for "".
func (d *Daemon) SetFitMode(mode, monitor string) *dbus.Error {
	fit, err := ParseFitMode(mode)
	if err != nil {
		return invalidArgs(err)
	}
	d.svc.SetFitMode(fit, monitor)
	return nil
}

// StartCycling starts cycling directory every intervalSecs.
func (d *Daemon) StartCycling(directory string, intervalSecs uint32, mode string) *dbus.Error {
	m, err := ParseCyclingMode(mode)
	if err != nil {
		return invalidArgs(err)
	}
	if err := d.svc.StartCycling(directory, time.Duration(intervalSecs)*time.Second, m); err != nil {
		return failed(err)
	}
	return nil
}

// StopCycling stops cycling.
func (d *Daemon) StopCycling() *dbus.Error {
	d.svc.StopCycling()
	return nil
}

// Next advances every monitor to its next cycle image.
func (d *Daemon) Next() *dbus.Error {
	d.svc.Advance()
	return nil
}

// Previous rewinds every monitor to its previous cycle image.
func (d *Daemon) Previous() *dbus.Error {
	d.svc.Rewind()
	return nil
}

// ExtractColors extracts colors from the theming wallpaper.
func (d *Daemon) ExtractColors() *dbus.Error {
	if err := d.svc.ExtractColors(context.Background()); err != nil {
		return failed(err)
	}
	return nil
}

// WallpaperForMonitor is a monitor's wallpaper path, "" when unknown or
// unset.
func (d *Daemon) WallpaperForMonitor(monitor string) (string, *dbus.Error) {
	path, _ := d.svc.Wallpaper(monitor)
	return path, nil
}

// GetFitMode is a monitor's fit mode; "fill" for an unknown monitor.
func (d *Daemon) GetFitMode(monitor string) (string, *dbus.Error) {
	fit, _ := d.svc.FitModeOf(monitor)
	return fit.String(), nil
}

// GetIsCycling reports whether cycling is active.
func (d *Daemon) GetIsCycling() (bool, *dbus.Error) {
	return d.svc.CyclingConfig() != nil, nil
}

// SetThemingMonitor picks the theming monitor; "" is the default.
func (d *Daemon) SetThemingMonitor(monitor string) *dbus.Error {
	d.svc.SetThemingMonitor(monitor)
	return nil
}

// RegisterMonitor registers a monitor.
func (d *Daemon) RegisterMonitor(monitor string) *dbus.Error {
	d.svc.RegisterMonitor(monitor)
	return nil
}

// UnregisterMonitor unregisters a monitor.
func (d *Daemon) UnregisterMonitor(monitor string) *dbus.Error {
	d.svc.UnregisterMonitor(monitor)
	return nil
}

// ListMonitors lists the registered monitors.
func (d *Daemon) ListMonitors() ([]string, *dbus.Error) {
	return d.svc.MonitorNames(), nil
}

// properties serves the interface's read-only properties, computed on
// each read like the zbus property getters (which never emit
// PropertiesChanged).
type properties struct{ svc *Service }

func (p properties) values() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"IsCycling":      dbus.MakeVariant(p.svc.CyclingConfig() != nil),
		"ThemingMonitor": dbus.MakeVariant(p.svc.ThemingMonitor()),
	}
}

// Get reads one property.
func (p properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != Interface {
		return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownInterface", Body: []any{"Unknown interface '" + iface + "'"}}
	}
	v, ok := p.values()[name]
	if !ok {
		return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty", Body: []any{"Unknown property '" + name + "'"}}
	}
	return v, nil
}

// GetAll reads every property of the interface.
func (p properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != Interface {
		return nil, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownInterface", Body: []any{"Unknown interface '" + iface + "'"}}
	}
	return p.values(), nil
}

// Set refuses: both properties are read-only.
func (p properties) Set(_, name string, _ dbus.Variant) *dbus.Error {
	return &dbus.Error{Name: "org.freedesktop.DBus.Error.PropertyReadOnly", Body: []any{"Property '" + name + "' is read-only"}}
}

// Client drives a running daemon (the WallpaperProxy the CLI uses).
type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// Connect dials the session bus.
func Connect() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to D-Bus session bus: %w", err) //nolint:staticcheck // the Rust CLI message
	}
	return NewClient(conn), nil
}

// NewClient wraps an existing connection.
func NewClient(conn *dbus.Conn) *Client {
	return &Client{conn: conn, obj: conn.Object(ServiceName, ServicePath)}
}

// Close drops the bus connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(ctx context.Context, method string, out any, args ...any) error {
	call := c.obj.CallWithContext(ctx, Interface+"."+method, 0, args...)
	if call.Err != nil || out == nil {
		return call.Err
	}
	return call.Store(out)
}

// SetWallpaper sets path on monitor ("" for all).
func (c *Client) SetWallpaper(ctx context.Context, path, monitor string) error {
	return c.call(ctx, "SetWallpaper", nil, path, monitor)
}

// SetFitMode sets the fit mode on monitor ("" for all).
func (c *Client) SetFitMode(ctx context.Context, mode, monitor string) error {
	return c.call(ctx, "SetFitMode", nil, mode, monitor)
}

// StartCycling starts cycling directory.
func (c *Client) StartCycling(ctx context.Context, directory string, intervalSecs uint32, mode string) error {
	return c.call(ctx, "StartCycling", nil, directory, intervalSecs, mode)
}

// StopCycling stops cycling.
func (c *Client) StopCycling(ctx context.Context) error { return c.call(ctx, "StopCycling", nil) }

// Next advances the cycle.
func (c *Client) Next(ctx context.Context) error { return c.call(ctx, "Next", nil) }

// Previous rewinds the cycle.
func (c *Client) Previous(ctx context.Context) error { return c.call(ctx, "Previous", nil) }

// ExtractColors re-runs color extraction.
func (c *Client) ExtractColors(ctx context.Context) error { return c.call(ctx, "ExtractColors", nil) }

// WallpaperForMonitor reads a monitor's wallpaper path.
func (c *Client) WallpaperForMonitor(ctx context.Context, monitor string) (string, error) {
	var path string
	err := c.call(ctx, "WallpaperForMonitor", &path, monitor)
	return path, err
}

// GetFitMode reads a monitor's fit mode.
func (c *Client) GetFitMode(ctx context.Context, monitor string) (string, error) {
	var mode string
	err := c.call(ctx, "GetFitMode", &mode, monitor)
	return mode, err
}

// GetIsCycling reads whether cycling is active.
func (c *Client) GetIsCycling(ctx context.Context) (bool, error) {
	var on bool
	err := c.call(ctx, "GetIsCycling", &on)
	return on, err
}

// SetThemingMonitor picks the theming monitor ("" for the default).
func (c *Client) SetThemingMonitor(ctx context.Context, monitor string) error {
	return c.call(ctx, "SetThemingMonitor", nil, monitor)
}

// ListMonitors lists the registered monitors.
func (c *Client) ListMonitors(ctx context.Context) ([]string, error) {
	var names []string
	err := c.call(ctx, "ListMonitors", &names)
	return names, err
}
