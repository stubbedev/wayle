// Package upower reads battery state from UPower over the system D-Bus,
// the Go counterpart of crates/wayle-battery's zbus proxy: the
// DisplayDevice aggregate by default, live updates through
// PropertiesChanged.
package upower

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// DeviceState mirrors UPower's device state enum
// (types.rs's DeviceState/From<u32>).
type DeviceState uint8

// Device states.
const (
	StateUnknown          DeviceState = iota
	StateCharging                     // 1
	StateDischarging                  // 2
	StateEmpty                        // 3
	StateFullyCharged                 // 4
	StatePendingCharge                // 5
	StatePendingDischarge             // 6
)

// StateFromUint32 maps UPower's wire value; anything else is Unknown.
func StateFromUint32(v uint32) DeviceState {
	switch v {
	case 1:
		return StateCharging
	case 2:
		return StateDischarging
	case 3:
		return StateEmpty
	case 4:
		return StateFullyCharged
	case 5:
		return StatePendingCharge
	case 6:
		return StatePendingDischarge
	}
	return StateUnknown
}

// WarningLevel mirrors UPower's warning level enum
// (types.rs's WarningLevel/From<u32>).
type WarningLevel uint8

// Warning levels.
const (
	WarningUnknown     WarningLevel = iota
	WarningNone                     // 1
	WarningDischarging              // 2, UPSes only
	WarningLow                      // 3
	WarningCritical                 // 4
	WarningAction                   // 5
)

// WarningFromUint32 maps UPower's wire value; anything else is Unknown.
func WarningFromUint32(v uint32) WarningLevel {
	if v >= 1 && v <= 5 {
		return WarningLevel(v)
	}
	return WarningUnknown
}

// Low reports Low, Critical, or Action: the levels the dropdown flags
// (helpers.rs's is_low_battery).
func (w WarningLevel) Low() bool {
	return w == WarningLow || w == WarningCritical || w == WarningAction
}

// Device is one battery snapshot: the org.freedesktop.UPower.Device
// properties the bar and its dropdown read.
type Device struct {
	Percentage  float64
	State       DeviceState
	TimeToEmpty time.Duration // 0 when unknown
	TimeToFull  time.Duration // 0 when unknown
	// IsPresent is UPower's IsPresent: a battery is in its bay.
	IsPresent bool
	// EnergyRate is the charge or discharge rate in watts.
	EnergyRate float64
	// Energy and EnergyFull are watt-hours now and at full charge.
	Energy     float64
	EnergyFull float64
	// Capacity is the health percentage (full against design); 0 when
	// unknown.
	Capacity     float64
	WarningLevel WarningLevel
	// ChargeEndThreshold is the charge limit percentage.
	ChargeEndThreshold       uint32
	ChargeThresholdSupported bool
	ChargeThresholdEnabled   bool
}

// deviceFromProps decodes a GetAll reply. A property that is missing or
// of the wrong type keeps its zero value: older UPower releases lack
// the charge-threshold properties.
func deviceFromProps(props map[string]dbus.Variant) Device {
	var d Device
	var state, warning uint32
	var tte, ttf int64
	read := func(name string, dst any) {
		if v, ok := props[name]; ok {
			_ = v.Store(dst)
		}
	}
	read("Percentage", &d.Percentage)
	read("State", &state)
	read("TimeToEmpty", &tte)
	read("TimeToFull", &ttf)
	read("IsPresent", &d.IsPresent)
	read("EnergyRate", &d.EnergyRate)
	read("Energy", &d.Energy)
	read("EnergyFull", &d.EnergyFull)
	read("Capacity", &d.Capacity)
	read("WarningLevel", &warning)
	read("ChargeEndThreshold", &d.ChargeEndThreshold)
	read("ChargeThresholdSupported", &d.ChargeThresholdSupported)
	read("ChargeThresholdEnabled", &d.ChargeThresholdEnabled)
	d.State = StateFromUint32(state)
	d.WarningLevel = WarningFromUint32(warning)
	d.TimeToEmpty = time.Duration(tte) * time.Second
	d.TimeToFull = time.Duration(ttf) * time.Second
	return d
}

// DisplayDevicePath is UPower's composite device across all batteries.
const DisplayDevicePath = "/org/freedesktop/UPower/devices/DisplayDevice"

const (
	busName             = "org.freedesktop.UPower"
	deviceInterface     = "org.freedesktop.UPower.Device"
	propertiesInterface = "org.freedesktop.DBus.Properties"
)

// Source is the seam the bar module and dropdown consume; tests swap
// in a fake.
type Source interface {
	// Read returns the current device snapshot.
	Read(ctx context.Context) (Device, error)
	// Subscribe delivers a tick per PropertiesChanged on the device.
	// The returned stop function unsubscribes; the channel closes on
	// ctx completion either way.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
	// EnableChargeThreshold turns the charge limit on or off
	// (Device::enable_charge_threshold).
	EnableChargeThreshold(ctx context.Context, enabled bool) error
}

// System reads the real UPower on the system bus.
type System struct {
	path dbus.ObjectPath
	conn *dbus.Conn
}

// NewSystem connects to the system bus and targets the DisplayDevice.
func NewSystem() (*System, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("upower: system bus: %w", err)
	}
	return NewOn(conn), nil
}

// NewOn targets the DisplayDevice over an existing connection.
func NewOn(conn *dbus.Conn) *System {
	return &System{path: dbus.ObjectPath(DisplayDevicePath), conn: conn}
}

// Close drops the bus connection.
func (s *System) Close() error { return s.conn.Close() }

// Read fetches every DisplayDevice property in one GetAll.
func (s *System) Read(ctx context.Context) (Device, error) {
	var props map[string]dbus.Variant
	err := s.conn.Object(busName, s.path).
		CallWithContext(ctx, propertiesInterface+".GetAll", 0, deviceInterface).Store(&props)
	if err != nil {
		return Device{}, fmt.Errorf("upower: GetAll: %w", err)
	}
	return deviceFromProps(props), nil
}

// EnableChargeThreshold calls the device's EnableChargeThreshold.
func (s *System) EnableChargeThreshold(ctx context.Context, enabled bool) error {
	call := s.conn.Object(busName, s.path).CallWithContext(ctx, deviceInterface+".EnableChargeThreshold", 0, enabled)
	if call.Err != nil {
		return fmt.Errorf("upower: EnableChargeThreshold: %w", call.Err)
	}
	return nil
}

// Subscribe matches PropertiesChanged for the DisplayDevice and ticks
// the channel per signal. The match is removed by stop.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(s.path),
		dbus.WithMatchInterface(propertiesInterface),
	); err != nil {
		return nil, nil, fmt.Errorf("upower: match signal: %w", err)
	}
	ticks := make(chan struct{}, 1)
	signals := make(chan *dbus.Signal, 16)
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
				if sig.Path != s.path || sig.Name != propertiesInterface+".PropertiesChanged" {
					continue
				}
				select {
				case ticks <- struct{}{}:
				default: // coalesce: one pending tick is enough
				}
			}
		}
	}()
	stop := func() {
		_ = s.conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(s.path),
			dbus.WithMatchInterface(propertiesInterface),
		)
		s.conn.RemoveSignal(signals)
	}
	return ticks, stop, nil
}
