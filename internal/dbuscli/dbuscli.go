// Package dbuscli turns D-Bus call failures into the CLI's
// user-facing messages (wayle/src/cli/dbus.rs's format_error): a
// missing service says to start the shell, a timeout says the service
// is not responding, and a method error shows the service's own
// message.
package dbuscli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// FormatError describes err from calling service's operation.
func FormatError(service, operation string, err error) error {
	name, msg, ok := dbusError(err)
	if !ok {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%s timed out - service not responding", operation)
		}
		return fmt.Errorf("Failed to %s: %w", operation, err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	switch {
	case strings.HasSuffix(name, ".ServiceUnknown") || strings.HasSuffix(name, ".NameHasNoOwner"):
		return fmt.Errorf("%s service not running. Start wayle shell first.", service) //nolint:staticcheck // the Rust CLI's sentence
	case strings.HasSuffix(name, ".NoReply") || strings.HasSuffix(name, ".Timeout"):
		return fmt.Errorf("%s timed out - service not responding", operation)
	}
	if msg == "" {
		msg = name
	}
	return fmt.Errorf("Failed to %s: %s", operation, msg) //nolint:staticcheck // the Rust CLI's capitalized message
}

// dbusError unpacks a D-Bus error reply: its name and first string
// argument.
func dbusError(err error) (name, msg string, ok bool) {
	if e, ok := errors.AsType[dbus.Error](err); ok {
		return e.Name, bodyMessage(e.Body), true
	}
	if pe, ok := errors.AsType[*dbus.Error](err); ok && pe != nil {
		return pe.Name, bodyMessage(pe.Body), true
	}
	return "", "", false
}

func bodyMessage(body []any) string {
	if len(body) > 0 {
		if s, ok := body[0].(string); ok {
			return s
		}
	}
	return ""
}
