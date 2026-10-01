// Package dbuscli turns D-Bus call failures into the CLI's
// user-facing messages (wayle/src/cli/dbus.rs's format_error).
//
// zbus surfaces a method's error reply as a MethodError: a name
// containing ServiceUnknown means the shell is down, anything else
// reads "Failed to <op>: <message>". A property read fails with an
// fdo::Error instead: ServiceUnknown and NameHasNoOwner mean down,
// NoReply and Timeout a timeout, and other fdo errors print their name
// before the message.
package dbuscli

import (
	"context"
	"errors"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Message is a user-facing CLI error: the Rust CLI's exact text,
// printed as "Error: <text>".
type Message string

func (m Message) Error() string { return string(m) }

// FormatError describes err from calling service's method for op.
func FormatError(service, op string, err error) error { return format(service, op, err, false) }

// FormatPropertyError describes err from reading service's property
// for op.
func FormatPropertyError(service, op string, err error) error {
	return format(service, op, err, true)
}

func format(service, op string, err error, property bool) error {
	notRunning := Message(service + " service not running. Start wayle shell first.")
	timedOut := Message(op + " timed out - service not responding")
	de, ok := replyError(err)
	if !ok {
		if errors.Is(err, context.DeadlineExceeded) {
			return timedOut
		}
		return Message("Failed to " + op + ": " + err.Error())
	}
	msg := de.Name
	if len(de.Body) > 0 {
		if s, ok := de.Body[0].(string); ok {
			msg = s
		}
	}
	if !property {
		if strings.Contains(de.Name, "ServiceUnknown") {
			return notRunning
		}
		return Message("Failed to " + op + ": " + msg)
	}
	switch de.Name {
	case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.NameHasNoOwner":
		return notRunning
	case "org.freedesktop.DBus.Error.NoReply", "org.freedesktop.DBus.Error.Timeout":
		return timedOut
	}
	if strings.HasPrefix(de.Name, "org.freedesktop.DBus.Error.") {
		return Message("Failed to " + op + ": " + de.Name + ": " + msg)
	}
	return Message("Failed to " + op + ": " + msg)
}

// replyError unpacks a D-Bus error reply, by value or by pointer.
func replyError(err error) (dbus.Error, bool) {
	if pe, ok := errors.AsType[*dbus.Error](err); ok && pe != nil {
		return *pe, true
	}
	return errors.AsType[dbus.Error](err)
}
