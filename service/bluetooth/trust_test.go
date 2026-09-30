package bluetooth

import (
	"context"
	"testing"

	"github.com/godbus/dbus/v5"
)

func setTrustedCall(p dbus.ObjectPath) string { return deviceIface + ".Set.Trusted " + string(p) }

func TestTrustPaired(t *testing.T) {
	f := populated(t)
	s := f.service()
	ctx := context.Background()

	// The paired, untrusted headset becomes trusted.
	if err := s.TrustPaired(ctx, dev1); err != nil {
		t.Fatalf("TrustPaired: %v", err)
	}
	if !f.called(setTrustedCall(dev1)) {
		t.Fatal("a paired device was not trusted")
	}
	waitFor(t, "trusted", func() bool { d, _ := s.State().Device(dev1); return d.Trusted })
	// An unpaired device is left alone: a stray trust would linger.
	if err := s.TrustPaired(ctx, dev2); err != nil {
		t.Fatalf("TrustPaired unpaired: %v", err)
	}
	if f.called(setTrustedCall(dev2)) {
		t.Error("an unpaired device was trusted")
	}
	// An unknown device is an error, not a silent no-op.
	if err := s.TrustPaired(ctx, "/org/bluez/hci0/dev_GONE"); err == nil {
		t.Error("unknown device: want an error")
	}
}

func TestTrustPairedSkipsTrustedDevices(t *testing.T) {
	f := startFakeBlueZ(t)
	props := deviceProps("AA:BB:CC:DD:EE:01", "Headset", hci0)
	props["Paired"] = dbus.MakeVariant(true)
	props["Trusted"] = dbus.MakeVariant(true)
	f.add(dev1, deviceIface, props, false)
	s := f.service()
	if err := s.TrustPaired(context.Background(), dev1); err != nil {
		t.Fatal(err)
	}
	if f.called(setTrustedCall(dev1)) {
		t.Error("an already trusted device was written again")
	}
}
