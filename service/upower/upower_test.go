package upower

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeDevice serves a DisplayDevice: GetAll over its props and the
// EnableChargeThreshold method, recording the last argument.
type fakeDevice struct {
	props   map[string]dbus.Variant
	enabled dbustest.Var[[]bool]
}

func (f *fakeDevice) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != deviceInterface {
		return nil, dbus.MakeFailedError(dbus.ErrMsgUnknownInterface)
	}
	return f.props, nil
}

func (f *fakeDevice) EnableChargeThreshold(enabled bool) *dbus.Error {
	f.enabled.Update(func(v []bool) []bool { return append(v, enabled) })
	return nil
}

func serveDevice(t *testing.T, bus *dbustest.Bus, props map[string]dbus.Variant) *fakeDevice {
	t.Helper()
	conn := bus.Conn(t)
	f := &fakeDevice{props: props}
	path := dbus.ObjectPath(DisplayDevicePath)
	if err := conn.ExportMethodTable(map[string]any{"GetAll": f.GetAll}, path, propertiesInterface); err != nil {
		t.Fatal(err)
	}
	if err := conn.ExportMethodTable(map[string]any{"EnableChargeThreshold": f.EnableChargeThreshold}, path, deviceInterface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", busName, reply, err)
	}
	return f
}

func TestReadDecodesEveryProperty(t *testing.T) {
	bus := dbustest.Start(t)
	serveDevice(t, bus, map[string]dbus.Variant{
		"Percentage":               dbus.MakeVariant(42.5),
		"State":                    dbus.MakeVariant(uint32(1)),
		"TimeToEmpty":              dbus.MakeVariant(int64(0)),
		"TimeToFull":               dbus.MakeVariant(int64(1800)),
		"IsPresent":                dbus.MakeVariant(true),
		"EnergyRate":               dbus.MakeVariant(12.3),
		"Energy":                   dbus.MakeVariant(20.0),
		"EnergyFull":               dbus.MakeVariant(55.0),
		"Capacity":                 dbus.MakeVariant(91.0),
		"WarningLevel":             dbus.MakeVariant(uint32(4)),
		"ChargeEndThreshold":       dbus.MakeVariant(uint32(80)),
		"ChargeThresholdSupported": dbus.MakeVariant(true),
		"ChargeThresholdEnabled":   dbus.MakeVariant(true),
	})
	dev, err := NewOn(bus.Conn(t)).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Device{
		Percentage: 42.5, State: StateCharging, TimeToFull: 30 * time.Minute,
		IsPresent: true, EnergyRate: 12.3, Energy: 20, EnergyFull: 55, Capacity: 91,
		WarningLevel: WarningCritical, ChargeEndThreshold: 80,
		ChargeThresholdSupported: true, ChargeThresholdEnabled: true,
	}
	if dev != want {
		t.Errorf("Read = %+v\nwant  %+v", dev, want)
	}
}

// Missing or mistyped properties (an older UPower) read as zero values,
// not an error.
func TestReadToleratesMissingAndMistypedProperties(t *testing.T) {
	bus := dbustest.Start(t)
	serveDevice(t, bus, map[string]dbus.Variant{
		"Percentage": dbus.MakeVariant(10.0),
		"State":      dbus.MakeVariant("charging"),
	})
	dev, err := NewOn(bus.Conn(t)).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dev != (Device{Percentage: 10}) {
		t.Errorf("Read = %+v, want only the percentage", dev)
	}
}

func TestReadErrorsWithoutUPower(t *testing.T) {
	bus := dbustest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := NewOn(bus.Conn(t)).Read(ctx); err == nil {
		t.Error("Read without a UPower on the bus: want an error")
	}
}

func TestEnableChargeThreshold(t *testing.T) {
	bus := dbustest.Start(t)
	f := serveDevice(t, bus, map[string]dbus.Variant{})
	sys := NewOn(bus.Conn(t))
	if err := sys.EnableChargeThreshold(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := sys.EnableChargeThreshold(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := f.enabled.Load(); len(got) != 2 || !got[0] || got[1] {
		t.Errorf("calls = %v, want [true false]", got)
	}
}

func TestWarningLevel(t *testing.T) {
	for raw, want := range map[uint32]WarningLevel{
		0: WarningUnknown, 1: WarningNone, 2: WarningDischarging,
		3: WarningLow, 4: WarningCritical, 5: WarningAction, 6: WarningUnknown,
	} {
		if got := WarningFromUint32(raw); got != want {
			t.Errorf("WarningFromUint32(%d) = %v, want %v", raw, got, want)
		}
	}
	for _, w := range []WarningLevel{WarningLow, WarningCritical, WarningAction} {
		if !w.Low() {
			t.Errorf("%v.Low() = false", w)
		}
	}
	for _, w := range []WarningLevel{WarningUnknown, WarningNone, WarningDischarging} {
		if w.Low() {
			t.Errorf("%v.Low() = true", w)
		}
	}
}
