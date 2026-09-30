package bluetooth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// populated is a fake with an unpowered hci0, a powered hci1, one
// connected headset with a battery, and one unpaired speaker.
func populated(t *testing.T) *fakeBlueZ {
	t.Helper()
	f := startFakeBlueZ(t)
	f.add(hci0, adapterIface, adapterProps("00:00:00:00:00:00", false), false)
	f.add(hci1, adapterIface, adapterProps("00:00:00:00:00:01", true), false)
	headset := deviceProps("AA:BB:CC:DD:EE:01", "Headset", hci0)
	headset["Connected"] = dbus.MakeVariant(true)
	headset["Paired"] = dbus.MakeVariant(true)
	f.add(dev1, deviceIface, headset, false)
	f.add(dev1, batteryIface, map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(80))}, false)
	f.add(dev2, deviceIface, deviceProps("AA:BB:CC:DD:EE:02", "Speaker", hci0), false)
	return f
}

func TestNewRegistersTheAgent(t *testing.T) {
	f := startFakeBlueZ(t)
	f.service()
	at, capability, isDefault := f.registration()
	if at != agentPath {
		t.Errorf("agent path = %q, want %q", at, agentPath)
	}
	if capability != "DisplayYesNo" {
		t.Errorf("capability = %q, want DisplayYesNo", capability)
	}
	if !isDefault {
		t.Error("the agent did not request default-agent status")
	}
}

func TestNewFailsWhenRegistrationIsRefused(t *testing.T) {
	f := startFakeBlueZ(t)
	f.mu.Lock()
	f.rejectRegister = true
	f.mu.Unlock()
	if s, err := New(f.bus.Conn(t)); err == nil {
		_ = s.Close()
		t.Fatal("refused RegisterAgent: want an error, got a service")
	}
}

func TestNewFailsWithoutBlueZ(t *testing.T) {
	f := startFakeBlueZ(t)
	// A bus with no org.bluez owner: registration cannot happen.
	_ = f.conn.Close()
	if s, err := New(f.bus.Conn(t)); err == nil {
		_ = s.Close()
		t.Fatal("no BlueZ: want an error, got a service")
	}
}

func TestDiscoveryReadsTheTree(t *testing.T) {
	f := populated(t)
	st := f.service().State()
	if len(st.Adapters) != 2 || len(st.Devices) != 2 {
		t.Fatalf("adapters %d devices %d, want 2 and 2", len(st.Adapters), len(st.Devices))
	}
	// find_best_adapter: the powered adapter wins over the first.
	if st.Primary == nil || st.Primary.Path != hci1 {
		t.Fatalf("primary = %+v, want hci1", st.Primary)
	}
	if !st.Available || !st.Enabled {
		t.Errorf("available %v enabled %v, want both", st.Available, st.Enabled)
	}
	if len(st.Connected) != 1 || st.Connected[0] != "AA:BB:CC:DD:EE:01" {
		t.Errorf("connected = %v, want the headset's address", st.Connected)
	}
	a := st.Adapters[0]
	if a.PowerState != PowerOff || a.DiscoverableTimeout != 180 || a.Modalias == nil ||
		len(a.Roles) != 2 || a.Roles[1] != RolePeripheral || a.Version != 12 {
		t.Errorf("adapter decode = %+v", a)
	}
	d, ok := st.Device(dev1)
	if !ok {
		t.Fatal("headset missing")
	}
	if d.BatteryPercentage == nil || *d.BatteryPercentage != 80 {
		t.Errorf("battery = %v, want 80", d.BatteryPercentage)
	}
	if d.RSSI == nil || *d.RSSI != -60 || d.Icon == nil || *d.Icon != "audio-headphones" ||
		d.Adapter != hci0 || string(d.ManufacturerData[76]) != "\x01\x02" {
		t.Errorf("device decode = %+v", d)
	}
	if other, _ := st.Device(dev2); other.BatteryPercentage != nil {
		t.Errorf("speaker battery = %v, want none", *other.BatteryPercentage)
	}
	snap := st.Snapshot()
	if !snap.Available || !snap.Enabled || len(snap.Connected) != 1 || snap.Connected[0] != "Headset" {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestNoAdapterIsUnavailable(t *testing.T) {
	f := startFakeBlueZ(t)
	s := f.service()
	st := s.State()
	if st.Available || st.Enabled || st.Primary != nil {
		t.Fatalf("empty tree state = %+v", st)
	}
	var noPrimary *NoPrimaryAdapterError
	if err := s.Enable(context.Background()); !errors.As(err, &noPrimary) || noPrimary.Operation != "enable bluetooth" {
		t.Errorf("Enable = %v, want NoPrimaryAdapterError", err)
	}
	if err := s.StartTimedDiscovery(context.Background(), time.Second); !errors.As(err, &noPrimary) {
		t.Errorf("StartTimedDiscovery = %v, want NoPrimaryAdapterError", err)
	}
}

func TestMonitoringFollowsDevices(t *testing.T) {
	f := populated(t)
	s := f.service()
	ticks, stop := s.Subscribe()
	defer stop()

	f.add("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_03", deviceIface, deviceProps("AA:BB:CC:DD:EE:03", "Mouse", hci0), true)
	waitFor(t, "the added device", func() bool { return len(s.State().Devices) == 3 })
	select {
	case <-ticks:
	case <-time.After(time.Second):
		t.Error("an added device did not tick subscribers")
	}

	f.update(dev2, deviceIface, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)})
	waitFor(t, "the connection", func() bool { return len(s.State().Connected) == 2 })

	// RSSI invalidated: the device left range.
	f.update(dev2, deviceIface, map[string]dbus.Variant{}, "RSSI")
	waitFor(t, "the invalidated RSSI", func() bool {
		d, _ := s.State().Device(dev2)
		return d.RSSI == nil
	})

	f.update(dev1, batteryIface, map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(42))})
	waitFor(t, "the battery change", func() bool {
		d, _ := s.State().Device(dev1)
		return d.BatteryPercentage != nil && *d.BatteryPercentage == 42
	})
	f.remove(dev1, batteryIface)
	waitFor(t, "the battery removal", func() bool {
		d, ok := s.State().Device(dev1)
		return ok && d.BatteryPercentage == nil
	})
	f.add(dev1, batteryIface, map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(7))}, true)
	waitFor(t, "the battery re-add", func() bool {
		d, _ := s.State().Device(dev1)
		return d.BatteryPercentage != nil && *d.BatteryPercentage == 7
	})

	f.remove(dev1, deviceIface, batteryIface)
	waitFor(t, "the removed device", func() bool {
		_, ok := s.State().Device(dev1)
		return !ok
	})
	if got := s.State().Connected; len(got) != 1 || got[0] != "AA:BB:CC:DD:EE:02" {
		t.Errorf("connected after removal = %v, want only the speaker", got)
	}
}

func TestMonitoringIgnoresForeignSignals(t *testing.T) {
	f := populated(t)
	s := f.service()
	// A peer that is not org.bluez announces a device and changes one:
	// the sender-scoped match drops both.
	impostor := f.bus.Conn(t)
	_ = impostor.Emit(objectManagerAt, objectManager+".InterfacesAdded", dbus.ObjectPath("/org/bluez/hci0/dev_FAKE"),
		map[string]map[string]dbus.Variant{deviceIface: deviceProps("FF:FF:FF:FF:FF:FF", "Fake", hci0)})
	_ = impostor.Emit(dev2, propertiesChange, deviceIface, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)}, []string{})
	// A real change after them proves the loop processed its queue.
	f.update(dev2, deviceIface, map[string]dbus.Variant{"Alias": dbus.MakeVariant("Renamed")})
	waitFor(t, "the real change", func() bool {
		d, _ := s.State().Device(dev2)
		return d.Alias == "Renamed"
	})
	st := s.State()
	if len(st.Devices) != 2 {
		t.Errorf("devices = %d, the impostor's device was added", len(st.Devices))
	}
	if d, _ := st.Device(dev2); d.Connected {
		t.Error("the impostor's property change was applied")
	}
	// Changes for unknown paths are ignored, not invented.
	f.update("/org/bluez/hci0/dev_UNKNOWN", deviceIface, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)})
	f.update(dev2, deviceIface, map[string]dbus.Variant{"Alias": dbus.MakeVariant("Again")})
	waitFor(t, "the second change", func() bool {
		d, _ := s.State().Device(dev2)
		return d.Alias == "Again"
	})
	if len(s.State().Devices) != 2 {
		t.Error("an unknown path's change created a device")
	}
}

func TestPrimaryAdapterFollowsTheAdapterList(t *testing.T) {
	f := startFakeBlueZ(t)
	f.add(hci0, adapterIface, adapterProps("00:00:00:00:00:00", false), false)
	s := f.service()
	// One unpowered adapter: it is primary (the first), but disabled.
	st := s.State()
	if st.Primary == nil || st.Primary.Path != hci0 || st.Enabled {
		t.Fatalf("state = %+v", st)
	}
	// A powered adapter appears: select_primary_adapter prefers it.
	f.add(hci1, adapterIface, adapterProps("00:00:00:00:00:01", true), true)
	waitFor(t, "the powered adapter as primary", func() bool {
		p := s.State().Primary
		return p != nil && p.Path == hci1
	})
	if !s.State().Enabled {
		t.Error("enabled follows the primary's Powered")
	}
	// Powering hci0 does not move the primary (only list changes do).
	f.update(hci0, adapterIface, map[string]dbus.Variant{"Powered": dbus.MakeVariant(true)})
	f.update(hci1, adapterIface, map[string]dbus.Variant{"Powered": dbus.MakeVariant(false)})
	waitFor(t, "hci1 powered off", func() bool { return !s.State().Enabled })
	if p := s.State().Primary; p == nil || p.Path != hci1 {
		t.Errorf("primary moved on a power change: %+v", p)
	}
	// The primary goes away: the remaining adapter takes over.
	f.remove(hci1, adapterIface)
	waitFor(t, "hci0 as primary", func() bool {
		p := s.State().Primary
		return p != nil && p.Path == hci0
	})
	f.remove(hci0, adapterIface)
	waitFor(t, "no adapter", func() bool { return !s.State().Available })
}

func TestSelectPrimary(t *testing.T) {
	off := &Adapter{Path: hci0}
	on := &Adapter{Path: hci1, Powered: true}
	cases := []struct {
		name     string
		current  dbus.ObjectPath
		adapters []*Adapter
		want     dbus.ObjectPath
	}{
		{"empty", hci0, nil, ""},
		{"no current picks powered", "", []*Adapter{off, on}, hci1},
		{"no current no powered picks first", "", []*Adapter{off}, hci0},
		{"gone current picks best", "/org/bluez/hci9", []*Adapter{off, on}, hci1},
		{"powered current stays", hci1, []*Adapter{off, on}, hci1},
		{"unpowered current yields to powered", hci0, []*Adapter{off, on}, hci1},
		{"unpowered current stays alone", hci0, []*Adapter{off}, hci0},
	}
	for _, c := range cases {
		if got := selectPrimary(c.current, c.adapters); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAdapterControls(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()

	if err := s.Disable(ctx); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	waitFor(t, "powered off", func() bool { return !s.State().Enabled })
	if err := s.Enable(ctx); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	waitFor(t, "powered on", func() bool { return s.State().Enabled })

	a := s.Adapter(hci1)
	for name, set := range map[string]func() error{
		"Discoverable":        func() error { return a.SetDiscoverable(ctx, true) },
		"DiscoverableTimeout": func() error { return a.SetDiscoverableTimeout(ctx, 60) },
		"Pairable":            func() error { return a.SetPairable(ctx, false) },
		"PairableTimeout":     func() error { return a.SetPairableTimeout(ctx, 30) },
		"Alias":               func() error { return a.SetAlias(ctx, "desk") },
		"Connectable":         func() error { return a.SetConnectable(ctx, false) },
	} {
		if err := set(); err != nil {
			t.Errorf("set %s: %v", name, err)
		}
	}
	waitFor(t, "the adapter settings", func() bool {
		p := s.State().Primary
		return p.Discoverable && p.DiscoverableTimeout == 60 && !p.Pairable &&
			p.PairableTimeout == 30 && p.Alias == "desk" && !p.Connectable
	})

	rssi := int16(-70)
	transport := TransportLE
	if err := a.SetDiscoveryFilter(ctx, DiscoveryFilterOptions{RSSI: &rssi, Transport: &transport}); err != nil {
		t.Fatalf("SetDiscoveryFilter: %v", err)
	}
	if got := f.lastFilter(); len(got) != 2 || got["RSSI"].Value() != int16(-70) || got["Transport"].Value() != "le" {
		t.Errorf("filter on the wire = %v", got)
	}
	filters, err := a.DiscoveryFilters(ctx)
	if err != nil || len(filters) == 0 {
		t.Errorf("DiscoveryFilters = %v, %v", filters, err)
	}
	path, err := a.ConnectDevice(ctx, map[string]dbus.Variant{"Address": dbus.MakeVariant("AA:BB:CC:DD:EE:01")})
	if err != nil || path != dev1 {
		t.Errorf("ConnectDevice = %q, %v", path, err)
	}
}

func TestAdapterControlErrors(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()
	// Stopping a discovery that never started is BlueZ's error, wrapped.
	err := s.StopDiscovery(ctx)
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Operation != "stop discovery" || errorName(err) != "org.bluez.Error.Failed" {
		t.Errorf("StopDiscovery = %v, want the wrapped org.bluez.Error.Failed", err)
	}
	// A failing Powered write surfaces instead of passing silently.
	f.failOn(adapterIface+".Set.Powered "+string(hci1), dbus.NewError("org.bluez.Error.Blocked", []any{"rfkill"}))
	if err := s.Enable(ctx); errorName(err) != "org.bluez.Error.Blocked" {
		t.Errorf("Enable = %v, want org.bluez.Error.Blocked", err)
	}
	// A read-only property cannot be written.
	if err := setProperty(ctx, s.conn, hci1, adapterIface, "set address", "Address", "x"); err == nil {
		t.Error("read-only Address write: want an error")
	}
}

func TestTimedDiscoveryStopsItself(t *testing.T) {
	f := populated(t)
	s := f.service()
	if err := s.StartTimedDiscovery(context.Background(), 50*time.Millisecond); err != nil {
		t.Fatalf("StartTimedDiscovery: %v", err)
	}
	waitFor(t, "discovering", func() bool { return s.State().Primary.Discovering })
	if !s.State().Snapshot().Discovering {
		t.Error("snapshot does not report discovery")
	}
	waitFor(t, "the timed stop", func() bool { return !s.State().Primary.Discovering })
	if !f.called("StopDiscovery " + string(hci1)) {
		t.Error("the timer did not stop the primary adapter's session")
	}
}

func TestStartDiscoveryFailureSkipsTheTimer(t *testing.T) {
	f := populated(t)
	s := f.service()
	f.failOn("StartDiscovery "+string(hci1), dbus.NewError("org.bluez.Error.NotReady", []any{"Resource Not Ready"}))
	if err := s.StartTimedDiscovery(context.Background(), 20*time.Millisecond); errorName(err) != "org.bluez.Error.NotReady" {
		t.Fatalf("StartTimedDiscovery = %v, want NotReady", err)
	}
	time.Sleep(60 * time.Millisecond)
	if f.called("StopDiscovery " + string(hci1)) {
		t.Error("a failed start still scheduled a stop")
	}
}

func TestDeviceControls(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()

	if err := s.Connect(ctx, dev2); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	waitFor(t, "connected", func() bool { d, _ := s.State().Device(dev2); return d.Connected })

	events, stop := s.Disconnected(dev2)
	defer stop()
	if err := s.Disconnect(ctx, dev2); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Reason != DisconnectLocal || ev.Message == "" {
			t.Errorf("disconnected event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Error("no Disconnected event")
	}
	waitFor(t, "disconnected", func() bool { d, _ := s.State().Device(dev2); return !d.Connected })

	if err := s.SetTrusted(ctx, dev2, true); err != nil {
		t.Fatalf("trust: %v", err)
	}
	waitFor(t, "trusted", func() bool { d, _ := s.State().Device(dev2); return d.Trusted })
	if err := s.SetTrusted(ctx, dev2, false); err != nil {
		t.Fatalf("untrust: %v", err)
	}
	waitFor(t, "untrusted", func() bool { d, _ := s.State().Device(dev2); return !d.Trusted })

	d := s.Device(dev2)
	for name, run := range map[string]func() error{
		"blocked":  func() error { return d.SetBlocked(ctx, true) },
		"wake":     func() error { return d.SetWakeAllowed(ctx, true) },
		"alias":    func() error { return d.SetAlias(ctx, "Kitchen") },
		"bearer":   func() error { return d.SetPreferredBearer(ctx, BearerLE) },
		"profile":  func() error { return d.ConnectProfile(ctx, "0000110b-0000-1000-8000-00805f9b34fb") },
		"unprofil": func() error { return d.DisconnectProfile(ctx, "0000110b-0000-1000-8000-00805f9b34fb") },
		"cancel":   func() error { return d.CancelPairing(ctx) },
	} {
		if err := run(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	waitFor(t, "the device settings", func() bool {
		d, _ := s.State().Device(dev2)
		return d.Blocked && d.WakeAllowed && d.Alias == "Kitchen" && d.PreferredBearer != nil && *d.PreferredBearer == BearerLE
	})
	for _, call := range []string{
		"ConnectProfile 0000110b-0000-1000-8000-00805f9b34fb " + string(dev2),
		"DisconnectProfile 0000110b-0000-1000-8000-00805f9b34fb " + string(dev2),
		"CancelPairing " + string(dev2),
	} {
		if !f.called(call) {
			t.Errorf("BlueZ did not see %q", call)
		}
	}
	if records, err := d.ServiceRecords(ctx); err != nil || len(records) != 1 {
		t.Errorf("ServiceRecords = %v, %v", records, err)
	}

	if err := s.Forget(ctx, dev2); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if !f.called("RemoveDevice " + string(dev2)) {
		t.Error("Forget did not go through the device's adapter")
	}
	waitFor(t, "forgotten", func() bool { _, ok := s.State().Device(dev2); return !ok })
}

func TestDeviceControlErrors(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()
	f.failOn("Connect "+string(dev2), dbus.NewError("org.bluez.Error.Failed", []any{"br-connection-page-timeout"}))
	err := s.Connect(ctx, dev2)
	var opErr *OperationError
	if !errors.As(err, &opErr) || opErr.Operation != "connect" || errorName(err) != "org.bluez.Error.Failed" {
		t.Errorf("Connect = %v, want the wrapped BlueZ failure", err)
	}
	if d, _ := s.State().Device(dev2); d.Connected {
		t.Error("a failed connect reads as connected")
	}
	// Forgetting a device BlueZ does not know fails at the property read.
	if err := s.Forget(ctx, "/org/bluez/hci0/dev_GONE"); err == nil {
		t.Error("forget of an unknown device: want an error")
	}
	// Disconnected streams only the device asked for.
	events, stop := s.Disconnected(dev1)
	defer stop()
	if err := s.Disconnect(ctx, dev2); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		t.Errorf("dev1's stream got dev2's event %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCloseEndsSubscriptions(t *testing.T) {
	f := populated(t)
	s := f.service()
	ticks, _ := s.Subscribe()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case _, ok := <-ticks:
		if ok {
			for range ticks {
			}
		}
	case <-time.After(time.Second):
		t.Fatal("Close left the subscription open")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}
