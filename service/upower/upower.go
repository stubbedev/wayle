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

// Device is one battery snapshot.
type Device struct {
	Percentage  float64
	State       DeviceState
	TimeToEmpty time.Duration // 0 when unknown
	TimeToFull  time.Duration // 0 when unknown
}

// Present reports whether a battery is there at all.
func (d Device) Present() bool { return d.State != StateUnknown || d.Percentage > 0 }

// DisplayDevicePath is UPower's composite device across all batteries.
const DisplayDevicePath = "/org/freedesktop/UPower/devices/DisplayDevice"

const (
	deviceInterface     = "org.freedesktop.UPower.Device"
	propertiesInterface = "org.freedesktop.DBus.Properties"
)

// Source is the read/subscribe seam the bar module consumes; tests
// swap in a fake.
type Source interface {
	// Read returns the current device snapshot.
	Read(ctx context.Context) (Device, error)
	// Subscribe delivers a tick per PropertiesChanged on the device.
	// The returned stop function unsubscribes; the channel closes on
	// ctx completion either way.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
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
	return &System{path: dbus.ObjectPath(DisplayDevicePath), conn: conn}, nil
}

// Close drops the bus connection.
func (s *System) Close() error { return s.conn.Close() }

// Read fetches the DisplayDevice properties in one round trip each —
// three property gets, the same calls the zbus proxy's cached
// properties issue on first access.
func (s *System) Read(ctx context.Context) (Device, error) {
	obj := s.conn.Object("org.freedesktop.UPower", s.path)
	var dev Device
	var percentage float64
	var state uint32
	var tte, ttf int64
	if err := obj.CallWithContext(ctx, propertiesInterface+".Get", 0, deviceInterface, "Percentage").Store(&percentage); err != nil {
		return dev, fmt.Errorf("upower: Percentage: %w", err)
	}
	if err := obj.CallWithContext(ctx, propertiesInterface+".Get", 0, deviceInterface, "State").Store(&state); err != nil {
		return dev, fmt.Errorf("upower: State: %w", err)
	}
	// TimeToEmpty/TimeToFull are 0 while charging/discharging
	// respectively; failures leave them unknown rather than fatal.
	_ = obj.CallWithContext(ctx, propertiesInterface+".Get", 0, deviceInterface, "TimeToEmpty").Store(&tte)
	_ = obj.CallWithContext(ctx, propertiesInterface+".Get", 0, deviceInterface, "TimeToFull").Store(&ttf)
	dev = Device{
		Percentage:  percentage,
		State:       StateFromUint32(state),
		TimeToEmpty: time.Duration(tte) * time.Second,
		TimeToFull:  time.Duration(ttf) * time.Second,
	}
	return dev, nil
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
