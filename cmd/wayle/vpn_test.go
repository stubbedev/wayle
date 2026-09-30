package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/internal/shellipc"
)

func TestVPNSSOCallbackReportsTheSignInCompleted(t *testing.T) {
	bus := dbustest.Start(t)
	var delivered string
	release, err := shellipc.Export(bus.Conn(t), shellipc.Handlers{VPNSSOCallback: func(uri string) bool {
		delivered = uri
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bus.UseAsSessionBus(t)
	var out bytes.Buffer
	if err := runVPN([]string{"sso-callback", "globalprotectcallback:prelogin-cookie=x"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Sign-in completed; you can close the browser tab.\n" {
		t.Errorf("output = %q", out.String())
	}
	if delivered != "globalprotectcallback:prelogin-cookie=x" {
		t.Errorf("delivered %q", delivered)
	}
}

func TestVPNSSOCallbackWithNothingWaitingFailsWithTheShellsReason(t *testing.T) {
	bus := dbustest.Start(t)
	release, err := shellipc.Export(bus.Conn(t), shellipc.Handlers{VPNSSOCallback: func(string) bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bus.UseAsSessionBus(t)
	var out bytes.Buffer
	err = runVPN([]string{"sso-callback", "globalprotectcallback:stale"}, &out)
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
	err := runVPN([]string{"sso-callback", "globalprotectcallback:x"}, &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), "VPN sign-in callback failed: ") {
		t.Errorf("err = %v", err)
	}
}

func TestVPNArgumentsAreChecked(t *testing.T) {
	for _, args := range [][]string{nil, {"sso-callback"}, {"sso-callback", "a", "b"}, {"connect"}} {
		if err := runVPN(args, &bytes.Buffer{}); err == nil {
			t.Errorf("wayle vpn %v succeeded", args)
		}
	}
}
