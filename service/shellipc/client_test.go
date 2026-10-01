package shellipc

import (
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestVPNSSOCallbackReachesTheWaitingSignIn(t *testing.T) {
	bus := dbustest.Start(t)
	var got []string
	waiting := true
	release, err := Serve(bus.Conn(t), NewState(nil), Hooks{VPNSSOCallback: func(uri string) bool {
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

func TestVPNSSOCallbackWithoutAHookHasNothingWaiting(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := Serve(bus.Conn(t), NewState(nil), Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := VPNSSOCallback(t.Context(), bus.Conn(t), "globalprotectcallback:x"); err == nil {
		t.Error("a shell with no sign-in accepted a callback")
	}
}

func TestVPNSSOCallbackWithNoShellRunningIsAnError(t *testing.T) {
	bus := dbustest.Start(t)
	if err := VPNSSOCallback(t.Context(), bus.Conn(t), "globalprotectcallback:x"); err == nil {
		t.Error("a callback with no shell on the bus succeeded")
	}
}
