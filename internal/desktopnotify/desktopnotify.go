// Package desktopnotify sends freedesktop notifications over the
// session bus, the Go counterpart of crates/wayle-shell-core's
// notify.rs: D-Bus directly rather than notify-send, whose absence
// from PATH would silently drop every notification.
package desktopnotify

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	busName = "org.freedesktop.Notifications"
	path    = "/org/freedesktop/Notifications"
	method  = busName + ".Notify"
)

// Sender delivers notifications on one bus connection.
type Sender struct {
	conn *dbus.Conn
}

// NewSender wraps an existing connection; the caller owns it.
func NewSender(conn *dbus.Conn) *Sender { return &Sender{conn: conn} }

// Send posts one notification with the daemon's default timeout and
// returns its id. appIcon is a themed icon name or an absolute path.
func (s *Sender) Send(ctx context.Context, appName, summary, body, appIcon string) (uint32, error) {
	var id uint32
	call := s.conn.Object(busName, path).CallWithContext(ctx, method, 0,
		appName, uint32(0), appIcon, summary, body, []string{}, map[string]dbus.Variant{}, int32(-1))
	if call.Err != nil {
		return 0, fmt.Errorf("desktopnotify: Notify: %w", call.Err)
	}
	if err := call.Store(&id); err != nil {
		return 0, fmt.Errorf("desktopnotify: Notify reply: %w", err)
	}
	return id, nil
}
