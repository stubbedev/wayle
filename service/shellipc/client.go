package shellipc

import (
	"context"
	"errors"
	"fmt"

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
