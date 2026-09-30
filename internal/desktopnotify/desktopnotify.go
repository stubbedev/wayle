// Package desktopnotify sends freedesktop notifications over the
// session bus, the Go counterpart of crates/wayle-shell-core's
// notify.rs: D-Bus directly rather than notify-send, whose absence
// from PATH would silently drop every notification.
package desktopnotify

import (
	"context"
	"fmt"
	"log"
	"time"

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
		appName, uint32(0), appIcon, summary, body, []string{}, iconHints(appIcon), int32(-1))
	if call.Err != nil {
		return 0, fmt.Errorf("desktopnotify: Notify: %w", call.Err)
	}
	if err := call.Store(&id); err != nil {
		return 0, fmt.Errorf("desktopnotify: Notify reply: %w", err)
	}
	return id, nil
}

// sendTimeout bounds Notify's fire-and-forget delivery.
const sendTimeout = 5 * time.Second

// iconHints carries appIcon in the image-path hint too, as libnotify
// does: wayle's own popup reads that hint in its default icon mode.
func iconHints(appIcon string) map[string]dbus.Variant {
	hints := map[string]dbus.Variant{}
	if appIcon != "" {
		hints["image-path"] = dbus.MakeVariant(appIcon)
	}
	return hints
}

// Notify is Send on a fresh session-bus connection, on its own
// goroutine: errors are logged, never returned, since a missing
// notification must not fail the caller's real work.
func Notify(appName, summary, body, appIcon string) {
	go func() {
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			log.Printf("notify: no session bus: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if _, err := NewSender(conn).Send(ctx, appName, summary, body, appIcon); err != nil {
			log.Printf("notify: %v", err)
		}
	}()
}
