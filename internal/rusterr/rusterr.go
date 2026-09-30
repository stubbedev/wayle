// Package rusterr renders Go errors the way the Rust code's Display
// impls print the equivalent errors, for messages that must read the
// same from both binaries.
package rusterr

import (
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// IO is std::io::Error's Display for an OS error: the C library's
// description and "(os error N)". Other errors print as they are.
func IO(err error) string {
	errno, ok := errors.AsType[syscall.Errno](err)
	if !ok {
		return err.Error()
	}
	desc := errno.Error()
	if desc != "" {
		desc = strings.ToUpper(desc[:1]) + desc[1:]
	}
	return fmt.Sprintf("%s (os error %d)", desc, int(errno))
}

// Zbus is zbus::Error's Display for a D-Bus error reply: "<name>:
// <message>", or the bare name without a message.
func Zbus(err error) string {
	de, ok := errors.AsType[dbus.Error](err)
	if !ok {
		if dep, isPtr := errors.AsType[*dbus.Error](err); isPtr && dep != nil {
			de, ok = *dep, true
		}
	}
	if !ok {
		return err.Error()
	}
	if len(de.Body) > 0 {
		if msg, isString := de.Body[0].(string); isString {
			return de.Name + ": " + msg
		}
	}
	return de.Name
}
