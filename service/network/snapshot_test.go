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
	if snap.WifiConnected || snap.WiredConnected || !snap.WifiEnabled {
		t.Fatalf("idle snapshot = %+v", snap)
	}

	ap := f.wifi.addAP(fakeAP{ssid: "home", strength: 71})
	f.wifi.associate(ap, "192.168.1.20")
	f.exportWired(1000, "10.0.0.5")
	snap, err = sys.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		WifiEnabled: true, WifiSSID: "home", WifiStrength: 71, WifiConnected: true,
		WifiFrequency: 2412, WifiIP4: "192.168.1.20",
		WiredConnected: true, WiredSpeed: 1000, WiredIP4: "10.0.0.5",
	}
	if snap != want {
		t.Errorf("snapshot = %+v\nwant       %+v", snap, want)
	}
}
