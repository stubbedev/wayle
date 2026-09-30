package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// dbusCallError turns a failed CLI call into the Rust CLI's message
// (wayle/src/cli/dbus.rs format_error): a service nobody owns says to
// start the shell, a timeout says so, and a daemon error reports its
// message.
func dbusCallError(service, operation string, err error) error { //nolint:unparam // shared by every D-Bus CLI; wallpaper is the first
	if dbusErr, ok := errors.AsType[dbus.Error](err); ok {
		if strings.Contains(dbusErr.Name, "ServiceUnknown") {
			return fmt.Errorf("%s service not running. Start wayle shell first.", service) //nolint:staticcheck // the Rust message
		}
		if strings.HasSuffix(dbusErr.Name, ".NoReply") || strings.HasSuffix(dbusErr.Name, ".Timeout") {
			return fmt.Errorf("%s timed out - service not responding", operation)
		}
		if len(dbusErr.Body) > 0 {
			if msg, ok := dbusErr.Body[0].(string); ok {
				return fmt.Errorf("Failed to %s: %s", operation, msg) //nolint:staticcheck // the Rust message
			}
		}
		return fmt.Errorf("Failed to %s: %s", operation, dbusErr.Name) //nolint:staticcheck // the Rust message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out - service not responding", operation)
	}
	return fmt.Errorf("Failed to %s: %w", operation, err) //nolint:staticcheck // the Rust message
}
