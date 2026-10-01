package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/shellipc"
)

func TestVPNSSOCallbackReportsTheSignInCompleted(t *testing.T) {
	bus := dbustest.Start(t)
	var delivered dbustest.Var[string]
	release, err := shellipc.Serve(bus.Conn(t), shellipc.NewState(nil), shellipc.Hooks{VPNSSOCallback: func(uri string) bool {
		delivered.Store(uri)
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bus.UseAsSessionBus(t)
	var out bytes.Buffer
	if err := runVPN(t, &out, "sso-callback", "globalprotectcallback:prelogin-cookie=x"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Sign-in completed; you can close the browser tab.\n" {
		t.Errorf("output = %q", out.String())
	}
	if got := delivered.Load(); got != "globalprotectcallback:prelogin-cookie=x" {
		t.Errorf("delivered %q", got)
	}
}

func TestVPNSSOCallbackWithNothingWaitingFailsWithTheShellsReason(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := shellipc.Serve(bus.Conn(t), shellipc.NewState(nil), shellipc.Hooks{VPNSSOCallback: func(string) bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bus.UseAsSessionBus(t)
	var out bytes.Buffer
	err = runVPN(t, &out, "sso-callback", "globalprotectcallback:stale")
	want := "VPN sign-in callback failed: org.freedesktop.DBus.Error.Failed: no VPN browser sign-in is waiting for a callback"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if out.Len() != 0 {
		t.Errorf("a failed callback printed %q", out.String())
	}
}

func TestVPNSSOCallbackWithNoShellRunningFails(t *testing.T) {
	bus := dbustest.Start(t)
	bus.UseAsSessionBus(t)
	err := runVPN(t, &bytes.Buffer{}, "sso-callback", "globalprotectcallback:x")
	if err == nil || !strings.HasPrefix(err.Error(), "VPN sign-in callback failed: ") {
		t.Errorf("err = %v", err)
	}
}

func TestVPNArgumentsAreChecked(t *testing.T) {
	for _, args := range [][]string{nil, {"sso-callback"}, {"sso-callback", "a", "b"}, {"connect"}} {
		if _, _, code := runCaptured(t, false, append([]string{"vpn"}, args...)...); code != 2 {
			t.Errorf("wayle vpn %v exited %d, want clap's usage error 2", args, code)
		}
	}
}

// runVPN runs `wayle vpn args...` through the command tree, returning
// the handler's error text (without the "Error: " prefix) as an error.
func runVPN(t *testing.T, out *bytes.Buffer, args ...string) error {
	t.Helper()
	stdout, stderr, code := runCaptured(t, false, append([]string{"vpn"}, args...)...)
	out.WriteString(stdout)
	if code != 0 {
		return errors.New(strings.TrimSuffix(strings.TrimPrefix(stderr, "Error: "), "\n"))
	}
	return nil
}
