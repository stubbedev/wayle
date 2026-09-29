// Package bluetooth reads BlueZ state over the system D-Bus: the
// default adapter's power, and the connected device list. The Go
// counterpart of crates/wayle-bluetooth's read path (pairing/agents
// stay in the dropdown, not ported here).
package bluetooth

import (
	"context"
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	bluezName     = "org.bluez"
	bluezRoot     = "/org/bluez"
	adapterIface  = "org.bluez.Adapter1"
	deviceIface   = "org.bluez.Device1"
	objectManager = "org.freedesktop.DBus.ObjectManager"
	properties    = "org.freedesktop.DBus.Properties"
)

// Snapshot is one read of BlueZ state.
type Snapshot struct {
	// Available reports whether a default adapter exists.
	Available bool
	// Enabled is the default adapter's Powered.
	Enabled bool
	// Connected holds the aliases of connected devices.
	Connected []string
}

// Source is the module's seam.
type Source interface {
	Read(ctx context.Context) (Snapshot, error)
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

// System reads the real BlueZ.
type System struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

// NewSystem connects to the system bus.
func NewSystem() (*System, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("bluetooth: system bus: %w", err)
	}
	return &System{conn: conn}, nil
}

// Close drops the bus connection.
func (s *System) Close() error { return s.conn.Close() }

// defaultAdapter finds the first org.bluez.Adapter1 in the object tree.
func (s *System) defaultAdapter(ctx context.Context) (dbus.ObjectPath, bool, error) {
	managed, err := s.managedObjects(ctx)
	if err != nil {
		return "", false, err
	}
	for path, ifaces := range managed {
		if _, ok := ifaces[adapterIface]; ok {
			return path, true, nil
		}
	}
	return "", false, nil
}

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

func (s *System) managedObjects(ctx context.Context) (managedObjects, error) {
	obj := s.conn.Object(bluezName, dbus.ObjectPath(bluezRoot))
	var out managedObjects
	if err := obj.CallWithContext(ctx, objectManager+".GetManagedObjects", 0).Store(&out); err != nil {
		return nil, fmt.Errorf("bluetooth: GetManagedObjects: %w", err)
	}
	return out, nil
}

// Read collects the snapshot.
func (s *System) Read(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{}
	adapter, ok, err := s.defaultAdapter(ctx)
	if err != nil {
		return snap, err
	}
	if !ok {
		return snap, nil
	}
	snap.Available = true

	adapterObj := s.conn.Object(bluezName, adapter)
	var powered bool
	if err := adapterObj.CallWithContext(ctx, properties+".Get", 0, adapterIface, "Powered").Store(&powered); err == nil {
		snap.Enabled = powered
	}

	managed, err := s.managedObjects(ctx)
	if err != nil {
		return snap, err
	}
	for _, ifaces := range managed {
		device, ok := ifaces[deviceIface]
		if !ok {
			continue
		}
		connected, _ := device["Connected"].Value().(bool)
		if !connected {
			continue
		}
		alias, _ := device["Alias"].Value().(string)
		if alias == "" {
			alias = "unknown"
		}
		snap.Connected = append(snap.Connected, alias)
	}
	return snap, nil
}

// Subscribe matches BlueZ property changes anywhere in the tree.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchInterface(properties),
		dbus.WithMatchArg(0, adapterIface),
	); err != nil {
		return nil, nil, fmt.Errorf("bluetooth: match adapter signal: %w", err)
	}
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchInterface(properties),
		dbus.WithMatchArg(0, deviceIface),
	); err != nil {
		return nil, nil, fmt.Errorf("bluetooth: match device signal: %w", err)
	}
	ticks := make(chan struct{}, 1)
	signals := make(chan *dbus.Signal, 32)
	s.conn.Signal(signals)
	go func() {
		defer close(ticks)
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if name, ok := sig.Body[1].(string); ok && name != adapterIface && name != deviceIface {
					continue
				}
				tick(ticks)
			}
		}
	}()
	stop := func() {
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchInterface(properties),
			dbus.WithMatchArg(0, adapterIface),
		)
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchInterface(properties),
			dbus.WithMatchArg(0, deviceIface),
		)
		s.conn.RemoveSignal(signals)
	}
	return ticks, stop, nil
}

func tick(ticks chan struct{}) {
	select {
	case ticks <- struct{}{}:
	default:
	}
}
