package bluetooth

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestAddressType(t *testing.T) {
	for _, s := range []string{"random", "Random", "RANDOM"} {
		if ParseAddressType(s) != AddressRandom {
			t.Errorf("%q: want random", s)
		}
	}
	for _, s := range []string{"public", "unknown", ""} {
		if ParseAddressType(s) != AddressPublic {
			t.Errorf("%q: want public", s)
		}
	}
	if AddressRandom.String() != "random" || AddressPublic.String() != "public" {
		t.Error("address type spelling")
	}
}

func TestPowerState(t *testing.T) {
	for s, want := range map[string]PowerState{
		"on": PowerOn, "off": PowerOff, "off-enabling": PowerOffToOn,
		"on-disabling": PowerOnToOff, "off-blocked": PowerOffBlocked,
	} {
		if got := ParsePowerState(s); got != want || got.String() != s {
			t.Errorf("%q: got %v (%q)", s, got, got.String())
		}
	}
	for _, s := range []string{"unknown", ""} {
		if ParsePowerState(s) != PowerOff {
			t.Errorf("%q: want off", s)
		}
	}
}

func TestAdapterRole(t *testing.T) {
	for s, want := range map[string]AdapterRole{
		"central": RoleCentral, "peripheral": RolePeripheral, "central-peripheral": RoleCentralPeripheral,
	} {
		if got := ParseAdapterRole(s); got != want || got.String() != s {
			t.Errorf("%q: got %v", s, got)
		}
	}
	if ParseAdapterRole("unknown") != RoleCentral || ParseAdapterRole("") != RoleCentral {
		t.Error("unknown roles read as central")
	}
}

func TestDiscoveryTransport(t *testing.T) {
	for s, want := range map[string]DiscoveryTransport{
		"auto": TransportAuto, "Auto": TransportAuto, "bredr": TransportBrEdr,
		"BREDR": TransportBrEdr, "le": TransportLE, "LE": TransportLE,
	} {
		if ParseDiscoveryTransport(s) != want {
			t.Errorf("%q: want %v", s, want)
		}
	}
	if ParseDiscoveryTransport("unknown") != TransportAuto || ParseDiscoveryTransport("") != TransportAuto {
		t.Error("unknown transports read as auto")
	}
}

func TestDiscoveryFilter(t *testing.T) {
	if f := (DiscoveryFilterOptions{}).Filter(); len(f) != 0 {
		t.Errorf("empty options = %v, want an empty filter", f)
	}
	rssi := int16(-70)
	if f := (DiscoveryFilterOptions{RSSI: &rssi}).Filter(); len(f) != 1 || f["RSSI"].Value() != int16(-70) {
		t.Errorf("rssi only = %v", f)
	}
	pathloss, le, yes, no, pattern := uint16(10), TransportLE, true, false, "dev"
	all := DiscoveryFilterOptions{
		UUIDs: []string{"0000110a-0000-1000-8000-00805f9b34fb"}, RSSI: &rssi, Pathloss: &pathloss,
		Transport: &le, DuplicateData: &yes, Discoverable: &no, Pattern: &pattern, AutoConnect: &yes,
	}.Filter()
	if len(all) != 8 {
		t.Fatalf("all options = %d keys, want 8", len(all))
	}
	for _, key := range []string{"UUIDs", "RSSI", "Pathloss", "Transport", "DuplicateData", "Discoverable", "Pattern", "AutoConnect"} {
		if _, ok := all[key]; !ok {
			t.Errorf("missing %s", key)
		}
	}
	if all["Transport"].Value() != "le" {
		t.Errorf("transport = %v, want the BlueZ spelling", all["Transport"].Value())
	}
}

func TestPreferredBearer(t *testing.T) {
	for s, want := range map[string]PreferredBearer{
		"last-used": BearerLastUsed, "bredr": BearerBrEdr, "le": BearerLE, "last-seen": BearerLastSeen,
	} {
		if got := ParsePreferredBearer(s); got != want || got.String() != s {
			t.Errorf("%q: got %v", s, got)
		}
	}
	if ParsePreferredBearer("unknown") != BearerLastUsed {
		t.Error("unknown bearer reads as last-used")
	}
}

func TestDisconnectReason(t *testing.T) {
	for s, want := range map[string]DisconnectReason{
		"org.bluez.Reason.Timeout":        DisconnectTimeout,
		"org.bluez.Reason.Local":          DisconnectLocal,
		"org.bluez.Reason.Remote":         DisconnectRemote,
		"org.bluez.Reason.Authentication": DisconnectAuthentication,
		"org.bluez.Reason.Suspend":        DisconnectSuspend,
	} {
		if ParseDisconnectReason(s) != want {
			t.Errorf("%q: want %v", s, want)
		}
	}
	if r := ParseDisconnectReason("unknown"); r != DisconnectUnknown || r.String() != "Unknown" {
		t.Errorf("unknown reason = %v", r)
	}
	if DisconnectTimeout.String() != "Connection timeout" {
		t.Error("reason text")
	}
}

func TestAgentCapability(t *testing.T) {
	for _, s := range []string{"DisplayYesNo", "DisplayOnly", "KeyboardOnly", "KeyboardDisplay", "NoInputNoOutput"} {
		if got := ParseAgentCapability(s).String(); got != s {
			t.Errorf("%q round-trips to %q", s, got)
		}
	}
	for _, s := range []string{"", "unknown", "invalid"} {
		if ParseAgentCapability(s) != CapabilityKeyboardDisplay {
			t.Errorf("%q: want KeyboardDisplay", s)
		}
	}
}

func TestApplyDeviceSkipsWrongTypes(t *testing.T) {
	d := Device{Alias: "kept"}
	// BlueZ never sends these shapes; a malformed value leaves the
	// field alone rather than zeroing it.
	applyDevice(&d, map[string]dbus.Variant{
		"Alias":     dbus.MakeVariant(uint32(5)),
		"Connected": dbus.MakeVariant("yes"),
		"RSSI":      dbus.MakeVariant("loud"),
	}, nil)
	if d.Alias != "kept" || d.Connected || d.RSSI != nil {
		t.Errorf("malformed values applied: %+v", d)
	}
	rank := uint8(2)
	applyDevice(&d, map[string]dbus.Variant{
		"Sets": dbus.MakeVariant(map[dbus.ObjectPath]map[string]dbus.Variant{
			"/org/bluez/set_1": {"Rank": dbus.MakeVariant(rank)},
		}),
		"AdvertisingData":  dbus.MakeVariant(map[uint8]dbus.Variant{0x16: dbus.MakeVariant([]byte{9})}),
		"ServiceData":      dbus.MakeVariant(map[string]dbus.Variant{"u": dbus.MakeVariant([]byte{1}), "bad": dbus.MakeVariant(3)}),
		"PreferredBearer":  dbus.MakeVariant("le"),
		"AdvertisingFlags": dbus.MakeVariant([]byte{6}),
	}, nil)
	if len(d.Sets) != 1 || d.Sets[0].Rank == nil || *d.Sets[0].Rank != 2 {
		t.Errorf("sets = %+v", d.Sets)
	}
	if string(d.AdvertisingData[0x16]) != "\x09" || len(d.ServiceData) != 1 || d.PreferredBearer == nil || *d.PreferredBearer != BearerLE {
		t.Errorf("maps = %+v", d)
	}
	applyDevice(&d, nil, []string{"PreferredBearer", "ServiceData"})
	if d.PreferredBearer != nil || d.ServiceData != nil {
		t.Error("invalidated properties survived")
	}
}

func TestApplyAdapterEmptyModaliasIsNone(t *testing.T) {
	a := Adapter{}
	applyAdapter(&a, map[string]dbus.Variant{"Modalias": dbus.MakeVariant("usb:x")}, nil)
	if a.Modalias == nil {
		t.Fatal("modalias not set")
	}
	applyAdapter(&a, map[string]dbus.Variant{"Modalias": dbus.MakeVariant("")}, nil)
	if a.Modalias != nil {
		t.Error("an empty modalias reads as set")
	}
}

func TestDisplayName(t *testing.T) {
	name := "Name"
	if (Device{Alias: "Alias", Name: &name}).DisplayName() != "Alias" {
		t.Error("alias wins")
	}
	if (Device{Name: &name}).DisplayName() != "Name" {
		t.Error("name is the fallback")
	}
	if (Device{}).DisplayName() != "" {
		t.Error("nothing to show is empty")
	}
}
