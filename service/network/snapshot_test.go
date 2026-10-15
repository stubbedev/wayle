package network

import (
	"context"
	"testing"
)

func TestReadSnapshotWifiAndWired(t *testing.T) {
	f := startFakeNM(t)
	sys := &System{conn: f.client()}
	snap, err := sys.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A disconnected wifi card is present and disconnected, not
	// connecting, and with no primary connection there is none.
	if want := (Snapshot{WifiEnabled: true, HasWifi: true}); snap != want {
		t.Fatalf("idle snapshot = %+v\nwant            %+v", snap, want)
	}

	ap := f.wifi.addAP(fakeAP{ssid: "home", strength: 71})
	f.wifi.associate(ap, "192.168.1.20")
	f.exportWired(1000, "10.0.0.5")
	f.setPrimary("802-11-wireless")
	snap, err = sys.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		WifiEnabled: true, Primary: ConnectionWifi,
		HasWifi: true, Wifi: StatusConnected, WifiSSID: "home", WifiStrength: 71,
		WifiFrequency: 2412, WifiIP4: "192.168.1.20",
		HasWired: true, Wired: StatusConnected, WiredSpeed: 1000, WiredIP4: "10.0.0.5",
	}
	if snap != want {
		t.Errorf("snapshot = %+v\nwant       %+v", snap, want)
	}
}

// Device states map as NetworkStatus::from_device_state does: only
// prepare through secondaries is connecting.
func TestStatusOf(t *testing.T) {
	for state, want := range map[DeviceState]Status{
		DeviceUnknown: StatusDisconnected, DeviceUnmanaged: StatusDisconnected,
		DeviceUnavailable: StatusDisconnected, DeviceDisconnected: StatusDisconnected,
		DevicePrepare: StatusConnecting, DeviceConfig: StatusConnecting, DeviceNeedAuth: StatusConnecting,
		DeviceIPConfig: StatusConnecting, DeviceIPCheck: StatusConnecting, DeviceSecondaries: StatusConnecting,
		DeviceActivated: StatusConnected, DeviceDeactivating: StatusDisconnected, DeviceFailed: StatusDisconnected,
	} {
		if got := statusOf(state); got != want {
			t.Errorf("statusOf(%d) = %d, want %d", state, got, want)
		}
	}
}

func TestConnectionTypeOf(t *testing.T) {
	for in, want := range map[string]ConnectionType{
		"": ConnectionNone, "802-11-wireless": ConnectionWifi, "802-3-ethernet": ConnectionWired,
		"vpn": ConnectionVPN, "wireguard": ConnectionWireGuard, "bluetooth": ConnectionBluetooth,
		"gsm": ConnectionOther,
	} {
		if got := connectionTypeOf(in); got != want {
			t.Errorf("connectionTypeOf(%q) = %d, want %d", in, got, want)
		}
	}
}
