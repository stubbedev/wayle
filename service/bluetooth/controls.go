package bluetooth

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// AdapterRef drives one adapter by path (core/adapter/controls.rs).
type AdapterRef struct {
	conn *dbus.Conn
	// Path is the adapter's object path.
	Path dbus.ObjectPath
}

// Adapter returns the controls for the adapter at path; the path is not
// checked until a call.
func (s *Service) Adapter(path dbus.ObjectPath) AdapterRef {
	return AdapterRef{conn: s.conn, Path: path}
}

// call runs one Adapter1 method, wrapping a failure as operation.
func (a AdapterRef) call(ctx context.Context, operation, method string, args ...any) *dbus.Call {
	call := a.conn.Object(bluezName, a.Path).CallWithContext(ctx, adapterIface+"."+method, 0, args...)
	if call.Err != nil {
		call.Err = &OperationError{Operation: operation, Err: call.Err}
	}
	return call
}

// set writes one Adapter1 property.
func (a AdapterRef) set(ctx context.Context, operation, name string, value any) error {
	return setProperty(ctx, a.conn, a.Path, adapterIface, operation, name, value)
}

// SetAlias sets the friendly name; empty reverts to the system name.
func (a AdapterRef) SetAlias(ctx context.Context, alias string) error {
	return a.set(ctx, "set alias", "Alias", alias)
}

// SetConnectable sets Connectable (false also clears Discoverable).
func (a AdapterRef) SetConnectable(ctx context.Context, connectable bool) error {
	return a.set(ctx, "set connectable", "Connectable", connectable)
}

// SetPowered switches the adapter on or off.
func (a AdapterRef) SetPowered(ctx context.Context, powered bool) error {
	return a.set(ctx, "set powered", "Powered", powered)
}

// SetDiscoverable makes the adapter visible or hidden.
func (a AdapterRef) SetDiscoverable(ctx context.Context, discoverable bool) error {
	return a.set(ctx, "set discoverable", "Discoverable", discoverable)
}

// SetDiscoverableTimeout sets the discoverable timeout in seconds (0:
// forever).
func (a AdapterRef) SetDiscoverableTimeout(ctx context.Context, seconds uint32) error {
	return a.set(ctx, "set discoverable timeout", "DiscoverableTimeout", seconds)
}

// SetPairable sets whether incoming pairing is accepted.
func (a AdapterRef) SetPairable(ctx context.Context, pairable bool) error {
	return a.set(ctx, "set pairable", "Pairable", pairable)
}

// SetPairableTimeout sets the pairable timeout in seconds (0: forever).
func (a AdapterRef) SetPairableTimeout(ctx context.Context, seconds uint32) error {
	return a.set(ctx, "set pairable timeout", "PairableTimeout", seconds)
}

// SetDiscoveryFilter sets this client's discovery filter; the zero
// options clear it.
func (a AdapterRef) SetDiscoveryFilter(ctx context.Context, options DiscoveryFilterOptions) error {
	return a.call(ctx, "set discovery filter", "SetDiscoveryFilter", options.Filter()).Err
}

// StartDiscovery opens this client's discovery session.
func (a AdapterRef) StartDiscovery(ctx context.Context) error {
	return a.call(ctx, "start discovery", "StartDiscovery").Err
}

// StopDiscovery closes this client's discovery session.
func (a AdapterRef) StopDiscovery(ctx context.Context) error {
	return a.call(ctx, "stop discovery", "StopDiscovery").Err
}

// RemoveDevice removes a device and its bonding.
func (a AdapterRef) RemoveDevice(ctx context.Context, device dbus.ObjectPath) error {
	return a.call(ctx, "remove device", "RemoveDevice", device).Err
}

// DiscoveryFilters lists the filter keys SetDiscoveryFilter accepts.
func (a AdapterRef) DiscoveryFilters(ctx context.Context) ([]string, error) {
	var filters []string
	call := a.call(ctx, "get discovery filters", "GetDiscoveryFilters")
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&filters); err != nil {
		return nil, &OperationError{Operation: "get discovery filters", Err: err}
	}
	return filters, nil
}

// ConnectDevice connects to an address without discovery (BlueZ
// experimental) and returns the device's path.
func (a AdapterRef) ConnectDevice(ctx context.Context, properties map[string]dbus.Variant) (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	call := a.call(ctx, "connect device", "ConnectDevice", properties)
	if call.Err != nil {
		return "", call.Err
	}
	if err := call.Store(&path); err != nil {
		return "", &OperationError{Operation: "connect device", Err: err}
	}
	return path, nil
}

// DeviceRef drives one device by path (core/device/controls.rs).
type DeviceRef struct {
	conn *dbus.Conn
	// Path is the device's object path.
	Path dbus.ObjectPath
}

// Device returns the controls for the device at path; the path is not
// checked until a call.
func (s *Service) Device(path dbus.ObjectPath) DeviceRef {
	return DeviceRef{conn: s.conn, Path: path}
}

func (d DeviceRef) call(ctx context.Context, operation, method string, args ...any) *dbus.Call {
	call := d.conn.Object(bluezName, d.Path).CallWithContext(ctx, deviceIface+"."+method, 0, args...)
	if call.Err != nil {
		call.Err = &OperationError{Operation: operation, Err: call.Err}
	}
	return call
}

func (d DeviceRef) set(ctx context.Context, operation, name string, value any) error {
	return setProperty(ctx, d.conn, d.Path, deviceIface, operation, name, value)
}

// Connect connects every auto-connectable profile.
func (d DeviceRef) Connect(ctx context.Context) error {
	return d.call(ctx, "connect", "Connect").Err
}

// Disconnect disconnects every profile.
func (d DeviceRef) Disconnect(ctx context.Context) error {
	return d.call(ctx, "disconnect", "Disconnect").Err
}

// ConnectProfile connects one profile by UUID.
func (d DeviceRef) ConnectProfile(ctx context.Context, uuid string) error {
	return d.call(ctx, "connect profile", "ConnectProfile", uuid).Err
}

// DisconnectProfile disconnects one profile by UUID.
func (d DeviceRef) DisconnectProfile(ctx context.Context, uuid string) error {
	return d.call(ctx, "disconnect profile", "DisconnectProfile", uuid).Err
}

// Pair pairs; BlueZ asks the agent along the way, so this blocks until
// the pairing prompt is answered.
func (d DeviceRef) Pair(ctx context.Context) error {
	return d.call(ctx, "pair", "Pair").Err
}

// CancelPairing aborts a Pair in progress.
func (d DeviceRef) CancelPairing(ctx context.Context) error {
	return d.call(ctx, "cancel pairing", "CancelPairing").Err
}

// ServiceRecords returns the raw SDP records.
func (d DeviceRef) ServiceRecords(ctx context.Context) ([][]byte, error) {
	var records [][]byte
	call := d.call(ctx, "get service records", "GetServiceRecords")
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&records); err != nil {
		return nil, &OperationError{Operation: "get service records", Err: err}
	}
	return records, nil
}

// SetTrusted sets Trusted (a trusted device's services need no
// authorization).
func (d DeviceRef) SetTrusted(ctx context.Context, trusted bool) error {
	return d.set(ctx, "set trusted", "Trusted", trusted)
}

// SetBlocked sets Blocked.
func (d DeviceRef) SetBlocked(ctx context.Context, blocked bool) error {
	return d.set(ctx, "set blocked", "Blocked", blocked)
}

// SetWakeAllowed sets WakeAllowed.
func (d DeviceRef) SetWakeAllowed(ctx context.Context, allowed bool) error {
	return d.set(ctx, "set wake allowed", "WakeAllowed", allowed)
}

// SetAlias sets the device's alias.
func (d DeviceRef) SetAlias(ctx context.Context, alias string) error {
	return d.set(ctx, "set alias", "Alias", alias)
}

// SetPreferredBearer sets PreferredBearer.
func (d DeviceRef) SetPreferredBearer(ctx context.Context, bearer PreferredBearer) error {
	return d.set(ctx, "set preferred bearer", "PreferredBearer", bearer.String())
}

// Forget removes the device through its adapter (RemoveDevice), reading
// the Adapter property live.
func (d DeviceRef) Forget(ctx context.Context) error {
	var adapter dbus.ObjectPath
	call := d.conn.Object(bluezName, d.Path).CallWithContext(ctx, propertiesIface+".Get", 0, deviceIface, "Adapter")
	if call.Err != nil {
		return &OperationError{Operation: "forget", Err: call.Err}
	}
	if err := call.Store(&adapter); err != nil {
		return &OperationError{Operation: "forget", Err: err}
	}
	return AdapterRef{conn: d.conn, Path: adapter}.RemoveDevice(ctx, d.Path)
}

// setProperty writes one BlueZ property through org.freedesktop.DBus.Properties.
func setProperty(ctx context.Context, conn *dbus.Conn, path dbus.ObjectPath, iface, operation, name string, value any) error {
	call := conn.Object(bluezName, path).CallWithContext(ctx, propertiesIface+".Set", 0, iface, name, dbus.MakeVariant(value))
	if call.Err != nil {
		return &OperationError{Operation: operation, Err: call.Err}
	}
	return nil
}
