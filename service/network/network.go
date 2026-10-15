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
	// wiredIface and ip4ConfigIface carry the link speed and addresses.
	wiredIface     = "org.freedesktop.NetworkManager.Device.Wired"
	ip4ConfigIface = "org.freedesktop.NetworkManager.IP4Config"
)

// Device types (NM_DEVICE_TYPE_*).
const (
	deviceEthernet = 1
	deviceWifi     = 2
)

// Status is a device's connectivity (NetworkStatus), ordered so the
// better of two is the greater.
type Status int

// Statuses.
const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
)

// statusOf is NetworkStatus::from_device_state: activated is
// connected, prepare through secondaries is connecting, and every
// other state (unmanaged, unavailable, disconnected, deactivating,
// failed) is disconnected.
func statusOf(s DeviceState) Status {
	switch s {
	case DeviceActivated:
		return StatusConnected
	case DevicePrepare, DeviceConfig, DeviceNeedAuth, DeviceIPConfig, DeviceIPCheck, DeviceSecondaries:
		return StatusConnecting
	}
	return StatusDisconnected
}

// ConnectionType is NM's PrimaryConnectionType
// (ConnectionType::from_nm_type).
type ConnectionType int

// Connection types; ConnectionOther is any type without its own.
const (
	ConnectionNone ConnectionType = iota
	ConnectionWifi
	ConnectionWired
	ConnectionVPN
	ConnectionWireGuard
	ConnectionBluetooth
	ConnectionOther
)

// connectionTypeOf parses a PrimaryConnectionType; "" is no primary
// connection.
func connectionTypeOf(nm string) ConnectionType {
	switch nm {
	case "":
		return ConnectionNone
	case "802-11-wireless":
		return ConnectionWifi
	case "802-3-ethernet":
		return ConnectionWired
	case "vpn":
		return ConnectionVPN
	case "wireguard":
		return ConnectionWireGuard
	case "bluetooth":
		return ConnectionBluetooth
	}
	return ConnectionOther
}

// Snapshot is one network state read.
type Snapshot struct {
	// WifiEnabled is NM's WirelessEnabled.
	WifiEnabled bool
	// Primary is the type of NM's primary connection.
	Primary ConnectionType
	// HasWifi and HasWired report a wireless and an ethernet device;
	// Wifi and Wired are their connectivity, the best device's when
	// there are several.
	HasWifi, HasWired bool
	Wifi, Wired       Status
	// The active access point's SSID, strength, and frequency (MHz),
	// and the device's first IPv4 address, when wifi is connected.
	WifiSSID      string
	WifiStrength  uint8
	WifiFrequency uint32
	WifiIP4       string
	// The link speed in Mb/s and first IPv4 address, when wired is
	// connected.
	WiredSpeed uint32
	WiredIP4   string
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
	var primary string
	if err := s.prop(ctx, nmIface, "PrimaryConnectionType", &primary); err != nil {
		return snap, fmt.Errorf("network: PrimaryConnectionType: %w", err)
	}
	snap.Primary = connectionTypeOf(primary)

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
		status := statusOf(DeviceState(state))
		switch deviceType {
		case deviceEthernet:
			snap.HasWired = true
			if status == StatusConnected && snap.Wired != StatusConnected {
				_ = dev.CallWithContext(ctx, properties+".Get", 0, wiredIface, "Speed").Store(&snap.WiredSpeed)
				snap.WiredIP4 = s.ip4(ctx, dev)
			}
			snap.Wired = max(snap.Wired, status)
		case deviceWifi:
			snap.HasWifi = true
			if status != StatusConnected || snap.Wifi == StatusConnected {
				snap.Wifi = max(snap.Wifi, status)
				continue
			}
			snap.Wifi = StatusConnected
			var ap dbus.ObjectPath
			// ActiveAccessPoint is a Device.Wireless property, not a
			// Device one.
			if err := dev.CallWithContext(ctx, properties+".Get", 0, wirelessIface, "ActiveAccessPoint").Store(&ap); err != nil || ap == "/" {
				continue
			}
			apObj := s.conn.Object(nmName, ap)
			var ssidBytes []byte
			var strength uint8
			if err := apObj.CallWithContext(ctx, properties+".Get", 0, apIface, "Ssid").Store(&ssidBytes); err != nil {
				continue
			}
			_ = apObj.CallWithContext(ctx, properties+".Get", 0, apIface, "Strength").Store(&strength)
			_ = apObj.CallWithContext(ctx, properties+".Get", 0, apIface, "Frequency").Store(&snap.WifiFrequency)
			snap.WifiIP4 = s.ip4(ctx, dev)
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

// ip4 is a device's first IPv4 address (Ip4Config's AddressData), ""
// when it has none.
func (s *System) ip4(ctx context.Context, dev dbus.BusObject) string {
	var cfg dbus.ObjectPath
	if err := dev.CallWithContext(ctx, properties+".Get", 0, deviceIface, "Ip4Config").Store(&cfg); err != nil || cfg == "/" || cfg == "" {
		return ""
	}
	var data []map[string]dbus.Variant
	if err := s.conn.Object(nmName, cfg).CallWithContext(ctx, properties+".Get", 0, ip4ConfigIface, "AddressData").Store(&data); err != nil {
		return ""
	}
	for _, entry := range data {
		if addr, ok := entry["address"].Value().(string); ok && addr != "" {
			return addr
		}
	}
	return ""
}
