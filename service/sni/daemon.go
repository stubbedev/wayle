package sni

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Wayle's own interface over the tray, which `wayle systray` drives
// (wayle-systray/src/dbus).
const (
	DaemonName = "com.wayle.SystemTray1"
	DaemonPath = dbus.ObjectPath("/com/wayle/SystemTray")
)

// Activator is the primary activation the daemon forwards to an item.
type Activator interface {
	Activate(ctx context.Context, it Item, x, y int32) error
}

// Daemon is the com.wayle.SystemTray1 object (SystemTrayDaemon).
type Daemon struct {
	store     *Store
	activator Activator
}

// ListRow is one List row: (id, title, icon name, status).
type ListRow struct {
	ID       string
	Title    string
	IconName string
	Status   string
}

// List snapshots the items.
func (d *Daemon) List() ([]ListRow, *dbus.Error) {
	items := d.store.Items()
	rows := make([]ListRow, len(items))
	for i, it := range items {
		rows[i] = ListRow{it.ID, it.Title, it.IconName, string(ParseStatus(string(it.Status)))}
	}
	return rows, nil
}

const activateTimeout = 25 * time.Second

// Activate activates the first item whose SNI Id is id at (0, 0).
func (d *Daemon) Activate(id string) *dbus.Error {
	for _, it := range d.store.Items() {
		if it.ID != id {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), activateTimeout)
		defer cancel()
		if err := d.activator.Activate(ctx, it, 0, 0); err != nil {
			return dbusx.Failed(activateError(err))
		}
		return nil
	}
	return dbusx.InvalidArgs("Tray item not found: " + id)
}

// activateError is controls.rs's mapping: an item without Activate is
// "not supported", anything else a failed operation.
func activateError(err error) string {
	var de dbus.Error
	if errors.As(err, &de) && de.Name == "org.freedesktop.DBus.Error.UnknownMethod" {
		return "tray item does not support 'activate', use its menu instead"
	}
	return "cannot perform tray operation 'activate'"
}

// ServeDaemon exports the interface over the store; isWatcher is the
// host's watcher role.
func ServeDaemon(conn *dbus.Conn, store *Store, activator Activator, isWatcher bool) (func(), error) {
	d := &Daemon{store: store, activator: activator}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      DaemonName,
		Path:      DaemonPath,
		Interface: DaemonName,
		Methods:   d,
		Properties: dbusx.Getters{
			"Count":     func() any { return uint32(len(store.Items())) },
			"IsWatcher": func() any { return isWatcher },
		},
	})
}
