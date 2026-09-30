package shellipc

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/godbus/dbus/v5"
)

// VPNSSOCallback hands a globalprotectcallback: URI to the running
// shell's waiting sign-in. A refusal reads "<error name>: <message>",
// the zbus Display the Rust CLI prints.
func VPNSSOCallback(ctx context.Context, conn *dbus.Conn, uri string) error {
	err := conn.Object(ServiceName, ServicePath).CallWithContext(ctx, ServiceName+".VpnSsoCallback", 0, uri).Err
	if e, ok := errors.AsType[dbus.Error](err); ok {
		return fmt.Errorf("%s: %s", e.Name, e.Error())
	}
	return err
}

// Lock asks the running shell to show its lock screen.
func Lock(ctx context.Context, conn *dbus.Conn) error {
	return conn.Object(ServiceName, ServicePath).CallWithContext(ctx, ServiceName+".Lock", 0).Err
}

// LockCommand is `wayle lock` and the wayle-lock binary
// (wayle/src/cli/lock.rs): lock through the running shell and print
// "Session locked"; the errors carry the Rust CLI's wording. connErr is
// the session-bus dial's error.
func LockCommand(ctx context.Context, conn *dbus.Conn, connErr error, stdout io.Writer) error {
	if connErr != nil {
		return fmt.Errorf("D-Bus session unavailable: %w", connErr)
	}
	if err := Lock(ctx, conn); err != nil {
		return fmt.Errorf("lock failed: %w", err)
	}
	_, err := fmt.Fprintln(stdout, "Session locked")
	return err
}
