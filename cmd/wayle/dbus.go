package main

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/dbuscli"
)

// cliMessage is a user-facing CLI error: the Rust CLI's exact text,
// printed as "Error: <text>".
type cliMessage = dbuscli.Message

// callTimeout bounds one D-Bus round trip (zbus's default method
// timeout is 25s, dbus-daemon's own reply timeout).
const callTimeout = 25 * time.Second

// daemonProxy is one of the shell's session-bus daemons as the CLI
// sees it (a zbus proxy plus wayle/src/cli/dbus.rs's error mapping).
type daemonProxy struct {
	label string // the service name in "... service not running"
	iface string
	conn  *dbus.Conn
	obj   dbus.BusObject
}

// connectDaemon is dbus.rs's session() plus the proxy.
func connectDaemon(label, name string, path dbus.ObjectPath, iface string) (*daemonProxy, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, cliMessage("Failed to connect to D-Bus session bus: " + err.Error())
	}
	return &daemonProxy{label: label, iface: iface, conn: conn, obj: conn.Object(name, path)}, nil
}

func (p *daemonProxy) Close() { _ = p.conn.Close() }

// withDaemon connects to one daemon around a handler (each Rust
// command's `let (_connection, proxy) = connect().await?`).
func withDaemon(label, name string, path dbus.ObjectPath, iface string, run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		p, err := connectDaemon(label, name, path, iface)
		if err != nil {
			return err
		}
		defer p.Close()
		return run(m, p)
	}
}

// call invokes a method, storing the reply into out; op names the
// operation in the error text ("Failed to <op>: ...").
func (p *daemonProxy) call(op, method string, out []any, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	c := p.obj.CallWithContext(ctx, p.iface+"."+method, 0, args...)
	err := c.Err
	if err == nil && len(out) > 0 {
		err = c.Store(out...)
	}
	if err != nil {
		return dbusError(p.label, op, err, false)
	}
	return nil
}

// prop reads a property into out.
func (p *daemonProxy) prop(op, name string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	err := p.obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, p.iface, name).Store(out)
	if err != nil {
		return dbusError(p.label, op, err, true)
	}
	return nil
}

// dbusError is dbus.rs's format_error for a method (property false) or
// a property read.
func dbusError(service, op string, err error, property bool) error {
	if property {
		return dbuscli.FormatPropertyError(service, op, err)
	}
	return dbuscli.FormatError(service, op, err)
}
