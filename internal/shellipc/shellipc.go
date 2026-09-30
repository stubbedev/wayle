// Package shellipc is the shell's com.wayle.Shell1 control object on
// the session bus, the Go counterpart of wayle-shell-core's
// services/shell_ipc/dbus.rs (served) and wayle-ipc's shell_ipc.rs
// (client). The Go shell serves VpnSsoCallback so far; BarHide,
// BarShow, BarToggle, Lock and the properties join as their handlers
// are ported, each as one more Handlers field and method.
package shellipc

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// D-Bus identity (wayle-ipc's SERVICE_NAME / SERVICE_PATH).
const (
	ServiceName = "com.wayle.Shell1"
	ServicePath = dbus.ObjectPath("/com/wayle/Shell")
	Interface   = "com.wayle.Shell1"
)

// errFailed is zbus's fdo::Error::Failed.
const errFailed = "org.freedesktop.DBus.Error.Failed"

// Handlers are what the running shell does for each method. A nil
// handler answers as though nothing were waiting on it.
type Handlers struct {
	// VPNSSOCallback hands a globalprotectcallback: URI to a waiting
	// GlobalProtect SAML sign-in, reporting whether one was waiting.
	VPNSSOCallback func(uri string) bool
}

// server is the exported object; its exported methods are the
// com.wayle.Shell1 interface and nothing else.
type server struct{ h Handlers }

// VpnSsoCallback hands the browser's globalprotectcallback: URI to the
// waiting sign-in. The desktop entry wayle installs for the scheme runs
// `wayle vpn sso-callback`, which calls this. A callback with nothing
// waiting is not something the browser can act on, but it is an error
// so the CLI does not report a stale callback as a sign-in that worked.
func (s server) VpnSsoCallback(uri string) *dbus.Error {
	if s.h.VPNSSOCallback != nil && s.h.VPNSSOCallback(uri) {
		return nil
	}
	return dbus.NewError(errFailed, []any{"no VPN browser sign-in is waiting for a callback"})
}

// Export serves the object on conn and takes the well-known name; the
// returned release drops the name.
func Export(conn *dbus.Conn, h Handlers) (release func(), err error) {
	if err := conn.Export(server{h: h}, ServicePath, Interface); err != nil {
		return nil, fmt.Errorf("shell ipc: export: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("shell ipc: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("shell ipc: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// VPNSSOCallback calls the running shell's VpnSsoCallback.
func VPNSSOCallback(ctx context.Context, conn *dbus.Conn, uri string) error {
	return describe(conn.Object(ServiceName, ServicePath).CallWithContext(ctx, Interface+".VpnSsoCallback", 0, uri).Err)
}

// describe renders a method error the way zbus displays one,
// "name: message", so the CLI's wording matches the Rust client's.
func describe(err error) error {
	if e, ok := errors.AsType[dbus.Error](err); ok {
		message := e.Error()
		return fmt.Errorf("%s: %s", e.Name, message)
	}
	return err
}
