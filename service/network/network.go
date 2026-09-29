// Package network reads NetworkManager state over the system D-Bus,
// the Go counterpart of crates/wayle-network's zbus proxy: overall
// state, wifi (enabled flag, active AP ssid and strength), wired
// (activated device), with StateChanged/PropertiesChanged ticks.
package network

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	nmName      = "org.freedesktop.NetworkManager"
	nmPath      = "/org/freedesktop/NetworkManager"
	nmIface     = "org.freedesktop.NetworkManager"
	properties  = "org.freedesktop.DBus.Properties"
	deviceIface = "org.freedesktop.NetworkManager.Device"
	apIface     = "org.freedesktop.NetworkManager.AccessPoint"
)

// Device types (NM_DEVICE_TYPE_*).
const (
	deviceEthernet = 1
	deviceWifi     = 2
	// deviceStateActivated is NM_DEVICE_STATE_ACTIVATED.
	deviceStateActivated = 100
)

// Status is the module-level network status.
type Status int

// Statuses (NetworkStatus in the Rust types).
const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
)

// Snapshot is one network state read.
type Snapshot struct {
	// WifiEnabled is NM's WirelessEnabled.
	WifiEnabled bool
	// Wifi is populated when a wifi device is activated.
	WifiSSID       string
	WifiStrength   uint8
	WifiConnected  bool
	WifiConnecting bool
	// WiredConnected reports an activated ethernet device.
	WiredConnected  bool
	WiredConnecting bool
}

// Source is the module's seam.
type Source interface {
	Read(ctx context.Context) (Snapshot, error)
	// Subscribe ticks on NM state changes. The channel closes when ctx
	// completes; stop releases the match.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

// System reads the real NetworkManager.
type System struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

// NewSystem connects to the system bus.
func NewSystem() (*System, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("network: system bus: %w", err)
	}
	return &System{conn: conn}, nil
}

// Close drops the bus connection.
func (s *System) Close() error { return s.conn.Close() }

func (s *System) prop(ctx context.Context, iface, name string, out any) error {
	obj := s.conn.Object(nmName, dbus.ObjectPath(nmPath))
	return obj.CallWithContext(ctx, properties+".Get", 0, iface, name).Store(out)
}

// Read collects the snapshot.
func (s *System) Read(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{}
	// WirelessEnabled is best-effort: a missing property means wifi is
	// absent, not fatal.
	_ = s.prop(ctx, nmIface, "WirelessEnabled", &snap.WifiEnabled)

	var devices []dbus.ObjectPath
	obj := s.conn.Object(nmName, dbus.ObjectPath(nmPath))
	if err := obj.CallWithContext(ctx, nmIface+".GetDevices", 0).Store(&devices); err != nil {
		return snap, fmt.Errorf("network: GetDevices: %w", err)
	}
	for _, devicePath := range devices {
		dev := s.conn.Object(nmName, devicePath)
		var deviceType uint32
		if err := dev.CallWithContext(ctx, properties+".Get", 0, deviceIface, "DeviceType").Store(&deviceType); err != nil {
			continue
		}
		var state uint32
		if err := dev.CallWithContext(ctx, properties+".Get", 0, deviceIface, "State").Store(&state); err != nil {
			continue
		}
		switch deviceType {
		case deviceEthernet:
			switch {
			case state == deviceStateActivated:
				snap.WiredConnected = true
			case state >= 10 && state < 100: // preparing..configuring
				snap.WiredConnecting = true
			}
		case deviceWifi:
			if state != deviceStateActivated {
				if state >= 10 && state < 100 {
					snap.WifiConnecting = true
				}
				continue
			}
			var ap dbus.ObjectPath
			if err := dev.CallWithContext(ctx, properties+".Get", 0, deviceIface, "ActiveAccessPoint").Store(&ap); err != nil || ap == "/" {
				continue
			}
			apObj := s.conn.Object(nmName, ap)
			var ssidBytes []byte
			var strength uint8
			if err := apObj.CallWithContext(ctx, properties+".Get", 0, apIface, "Ssid").Store(&ssidBytes); err != nil {
				continue
			}
			_ = apObj.CallWithContext(ctx, properties+".Get", 0, apIface, "Strength").Store(&strength)
			snap.WifiConnected = true
			snap.WifiSSID = strings.TrimRight(string(ssidBytes), "\x00")
			snap.WifiStrength = strength
		}
	}
	return snap, nil
}

// Subscribe matches NM's StateChanged plus property changes on the
// manager object.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(nmPath)),
		dbus.WithMatchArg(0, nmIface),
	); err != nil {
		return nil, nil, fmt.Errorf("network: match signal: %w", err)
	}
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(nmPath)),
		dbus.WithMatchInterface(nmIface),
	); err != nil {
		return nil, nil, fmt.Errorf("network: match state signal: %w", err)
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
				switch sig.Name {
				case properties + ".PropertiesChanged", nmIface + ".StateChanged":
					tick(ticks)
				}
			}
		}
	}()
	stop := func() {
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(nmPath)),
			dbus.WithMatchArg(0, nmIface),
		)
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(nmPath)),
			dbus.WithMatchInterface(nmIface),
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

// SignalIndex is helpers.rs's signal_to_index: the wifi strength
// bucketed over the icon list length. Exported for the module; pure.
func SignalIndex(strength uint8, numIcons int) int {
	if numIcons == 0 {
		return 0
	}
	clamped := min(int(strength), 100)
	bucket := max(100/numIcons, 1)
	return min(clamped/bucket, numIcons-1)
}

var _ = time.Second
