// Package portal is wayle's xdg-desktop-portal backend
// (crates/wayle-portal): the org.freedesktop.impl.portal.* interfaces
// the compositor-independent xdg-desktop-portal frontend routes
// sandboxed apps' requests to, served as one object under the
// org.freedesktop.impl.portal.desktop.wayle name that `wayle portal`
// claims. Interactive interfaces delegate their dialogs to the running
// shell over its own D-Bus services.
package portal

import (
	"fmt"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/dbusx"
)

// BusName is the backend's well-known name (wayle.portal's DBusName).
const BusName = "org.freedesktop.impl.portal.desktop.wayle"

// ObjectPath is where every interface is mounted.
const ObjectPath = dbus.ObjectPath("/org/freedesktop/portal/desktop")

// Backend is the running portal backend.
type Backend struct {
	conn     *dbus.Conn
	cfg      *config.Service
	sessions *sessions
	// spawn starts a program the portal hands off to (detach; a
	// recorder in tests).
	spawn func(argv []string) error
}

// New builds the backend over a session-bus connection and the config.
func New(conn *dbus.Conn, cfg *config.Service) *Backend {
	return &Backend{conn: conn, cfg: cfg, sessions: newSessions(conn), spawn: detach}
}

// interfaces is every interface the backend mounts at ObjectPath.
func (b *Backend) interfaces() []dbusx.Interface {
	return []dbusx.Interface{
		settingsIface(b.cfg),
		lockdownIface(),
		backgroundIface(),
		usbIface(),
		emailIface(b.spawn),
		secretIface(),
	}
}

// Serve mounts the interfaces, claims BusName (failing loudly when
// another backend owns it rather than queueing and idling forever) and
// starts the settings watcher. The returned stop ends the watcher, runs
// every live session's cleanup and drops the name.
func (b *Backend) Serve() (stop func(), err error) {
	if _, err := dbusx.Export(b.conn, ObjectPath, b.interfaces()...); err != nil {
		return nil, fmt.Errorf("cannot register D-Bus object: %w", err)
	}
	release, err := dbusx.Own(b.conn, BusName)
	if err != nil {
		return nil, fmt.Errorf("cannot request D-Bus name: %w", err)
	}
	unwatch := watchSettings(b.conn, b.cfg)
	return func() {
		unwatch()
		b.sessions.clearAll()
		release()
	}, nil
}
