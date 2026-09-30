package shellipc

import (
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestVpnSsoCallbackReachesTheWaitingSignIn(t *testing.T) {
	bus := dbustest.Start(t)
	var got []string
	waiting := true
	release, err := Export(bus.Conn(t), Handlers{VPNSSOCallback: func(uri string) bool {
		got = append(got, uri)
		return waiting
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	client := bus.Conn(t)
	if err := VPNSSOCallback(t.Context(), client, "globalprotectcallback:abc"); err != nil {
		t.Fatalf("a waiting sign-in refused the callback: %v", err)
	}
	waiting = false
	err = VPNSSOCallback(t.Context(), client, "globalprotectcallback:stale")
	want := "org.freedesktop.DBus.Error.Failed: no VPN browser sign-in is waiting for a callback"
	if err == nil || err.Error() != want {
		t.Errorf("stale callback: err = %v, want %q", err, want)
	}
	if len(got) != 2 || got[0] != "globalprotectcallback:abc" {
		t.Errorf("handler saw %v", got)
	}
}

func TestANilHandlerHasNothingWaiting(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := Export(bus.Conn(t), Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := VPNSSOCallback(t.Context(), bus.Conn(t), "globalprotectcallback:x"); err == nil {
		t.Error("a shell with no sign-in accepted a callback")
	}
}

func TestASecondShellCannotTakeTheName(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := Export(bus.Conn(t), Handlers{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Export(bus.Conn(t), Handlers{}); err == nil {
		t.Error("two shells owned com.wayle.Shell1")
	}
}

func TestNoShellRunningIsAnError(t *testing.T) {
	bus := dbustest.Start(t)
	if err := VPNSSOCallback(t.Context(), bus.Conn(t), "globalprotectcallback:x"); err == nil {
		t.Error("a callback with no shell on the bus succeeded")
	}
}
