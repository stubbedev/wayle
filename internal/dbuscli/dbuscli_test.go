package dbuscli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestFormatErrorForMethods(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"service unknown", dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, "Screenshot service not running. Start wayle shell first."},
		{"by pointer", &dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}, "Screenshot service not running. Start wayle shell first."},
		{"wrapped", fmt.Errorf("call: %w", dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}), "Screenshot service not running. Start wayle shell first."},
		// A method error is a MethodError: only ServiceUnknown is special.
		{"no owner is a plain failure", dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}, "Failed to capture screenshot: org.freedesktop.DBus.Error.NameHasNoOwner"},
		{"message shown", dbus.Error{Name: "org.freedesktop.DBus.Error.Failed", Body: []any{"no outputs available"}}, "Failed to capture screenshot: no outputs available"},
		{"bodiless shows its name", dbus.Error{Name: "com.example.Oops"}, "Failed to capture screenshot: com.example.Oops"},
		{"client deadline", fmt.Errorf("call: %w", context.DeadlineExceeded), "capture screenshot timed out - service not responding"},
		{"transport error", errors.New("broken pipe"), "Failed to capture screenshot: broken pipe"},
	} {
		if got := FormatError("Screenshot", "capture screenshot", tc.err).Error(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatErrorForProperties(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no owner", dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"}, "Audio service not running. Start wayle shell first."},
		{"no reply", dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"}, "get volume timed out - service not responding"},
		{"timeout", dbus.Error{Name: "org.freedesktop.DBus.Error.Timeout"}, "get volume timed out - service not responding"},
		{"fdo error names itself", dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty", Body: []any{"nope"}}, "Failed to get volume: org.freedesktop.DBus.Error.UnknownProperty: nope"},
		{"custom error", dbus.Error{Name: "com.example.Oops", Body: []any{"bad"}}, "Failed to get volume: bad"},
	} {
		if got := FormatPropertyError("Audio", "get volume", tc.err).Error(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
