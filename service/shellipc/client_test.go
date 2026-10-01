package shellipc

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestVPNSSOCallbackReachesTheWaitingSignIn(t *testing.T) {
	bus := dbustest.Start(t)
	var got dbustest.Var[[]string]
	var waiting dbustest.Var[bool]
	waiting.Store(true)
	release, err := Serve(bus.Conn(t), NewState(nil), Hooks{VPNSSOCallback: func(uri string) bool {
		got.Update(func(s []string) []string { return append(s, uri) })
		return waiting.Load()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	client := bus.Conn(t)
	if err := VPNSSOCallback(t.Context(), client, "globalprotectcallback:abc"); err != nil {
		t.Fatalf("a waiting sign-in refused the callback: %v", err)
	}
	waiting.Store(false)
	err = VPNSSOCallback(t.Context(), client, "globalprotectcallback:stale")
	want := "org.freedesktop.DBus.Error.Failed: no VPN browser sign-in is waiting for a callback"
	if err == nil || err.Error() != want {
		t.Errorf("stale callback: err = %v, want %q", err, want)
	}
	if seen := got.Load(); len(seen) != 2 || seen[0] != "globalprotectcallback:abc" {
		t.Errorf("handler saw %v", seen)
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

// TestLockMethod pins the Lock round trip over a real bus: a ready
// lock screen succeeds, a not-ready one (or none) fails with the Rust
// daemon's Failed error.
func TestLockMethod(t *testing.T) {
	bus := dbustest.Start(t)
	var ready dbustest.Var[bool]
	var calls dbustest.Var[int]
	release, err := Serve(bus.Conn(t), NewState(nil), Hooks{Lock: func() bool { calls.Update(func(n int) int { return n + 1 }); return ready.Load() }})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer release()
	client := bus.Conn(t)
	err = Lock(t.Context(), client)
	if err == nil || !strings.Contains(err.Error(), "lock screen not ready") {
		t.Fatalf("Lock before ready = %v, want the not-ready failure", err)
	}
	ready.Store(true)
	if err := Lock(t.Context(), client); err != nil {
		t.Fatalf("Lock when ready: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("handler calls = %d, want 2", n)
	}
}

// TestLockCommand pins the CLI's output and error wording.
func TestLockCommand(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := Serve(bus.Conn(t), NewState(nil), Hooks{Lock: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := LockCommand(t.Context(), bus.Conn(t), nil, &out); err != nil {
		t.Fatalf("LockCommand: %v", err)
	}
	if out.String() != "Session locked\n" {
		t.Errorf("stdout = %q", out.String())
	}
	out.Reset()
	err = LockCommand(t.Context(), nil, errors.New("no bus"), &out)
	if err == nil || err.Error() != "D-Bus session unavailable: no bus" || out.Len() != 0 {
		t.Errorf("no bus: %v, stdout %q", err, out.String())
	}
	release()
	err = LockCommand(t.Context(), bus.Conn(t), nil, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "lock failed: ") || out.Len() != 0 {
		t.Errorf("no shell: %v, stdout %q", err, out.String())
	}
}
