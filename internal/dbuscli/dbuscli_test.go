package dbuscli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestFormatError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			"service unknown",
			dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"},
			"Screenshot service not running. Start wayle shell first.",
		},
		{
			"no owner", &dbus.Error{Name: "org.freedesktop.DBus.Error.NameHasNoOwner"},
			"Screenshot service not running. Start wayle shell first.",
		},
		{
			"no reply",
			dbus.Error{Name: "org.freedesktop.DBus.Error.NoReply"},
			"capture screenshot timed out - service not responding",
		},
		{
			"a method error shows its message",
			dbus.Error{Name: "org.freedesktop.DBus.Error.Failed", Body: []any{"no outputs available"}},
			"Failed to capture screenshot: no outputs available",
		},
		{
			"a bodiless method error shows its name",
			dbus.Error{Name: "com.example.Oops"},
			"Failed to capture screenshot: com.example.Oops",
		},
		{
			"a wrapped reply still classifies", fmt.Errorf("call: %w", dbus.Error{Name: "org.freedesktop.DBus.Error.ServiceUnknown"}),
			"Screenshot service not running. Start wayle shell first.",
		},
		{
			"a client deadline reads as a timeout", fmt.Errorf("call: %w", context.DeadlineExceeded),
			"capture screenshot timed out - service not responding",
		},
		{
			"a transport error passes through", errors.New("broken pipe"),
			"Failed to capture screenshot: broken pipe",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatError("Screenshot", "capture screenshot", tc.err).Error(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
