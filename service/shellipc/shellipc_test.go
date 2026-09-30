package shellipc

import (
	"errors"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/internal/dbusx/dbustest"
)

func TestBarVisibilityMatchesBarRS(t *testing.T) {
	var seen []map[string]bool
	s := NewState(func(h map[string]bool) { seen = append(seen, h) })
	s.SetConnectors([]string{"DP-1", "HDMI-A-1"})

	s.Hide("DP-1")
	if !s.Hidden("DP-1") || s.Hidden("HDMI-A-1") {
		t.Fatalf("hide one: %v", s.hiddenSorted())
	}
	s.Hide("DP-9") // unknown connectors are ignored, without a change
	if n := len(seen); n != 1 {
		t.Errorf("an ignored hide notified: %d", n)
	}
	s.Toggle("") // something hidden: toggle-all shows everything
	if len(s.hiddenSorted()) != 0 {
		t.Errorf("toggle all with one hidden: %v", s.hiddenSorted())
	}
	s.Toggle("") // nothing hidden: toggle-all hides everything
	if !slices.Equal(s.hiddenSorted(), []string{"DP-1", "HDMI-A-1"}) {
		t.Errorf("toggle all with none hidden: %v", s.hiddenSorted())
	}
	s.Show("HDMI-A-1")
	s.Toggle("DP-1")
	if len(s.hiddenSorted()) != 0 {
		t.Errorf("show + toggle: %v", s.hiddenSorted())
	}
	s.Hide("")
	s.SetConnectors([]string{"DP-1"}) // a vanished bar drops out of the hidden set
	if !slices.Equal(s.hiddenSorted(), []string{"DP-1"}) {
		t.Errorf("pruned: %v", s.hiddenSorted())
	}
	s.Show("")
	if len(s.hiddenSorted()) != 0 {
		t.Errorf("show all: %v", s.hiddenSorted())
	}
}

func TestShell1DaemonProperties(t *testing.T) {
	dbustest.SessionBus(t)
	s := NewState(nil)
	s.SetConnectors([]string{"HDMI-A-1", "DP-1"})
	release, err := Serve(dbustest.Conn(t), s, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	obj := dbustest.Conn(t).Object(ServiceName, ServicePath)
	if err := obj.Call(ServiceName+".BarHide", 0, "").Err; err != nil {
		t.Fatal(err)
	}
	v, err := obj.GetProperty(ServiceName + ".BarHidden")
	if err != nil || !slices.Equal(v.Value().([]string), []string{"DP-1", "HDMI-A-1"}) {
		t.Errorf("BarHidden = %v, %v (sorted)", v, err)
	}
	v, err = obj.GetProperty(ServiceName + ".Connectors")
	if err != nil || !slices.Equal(v.Value().([]string), []string{"HDMI-A-1", "DP-1"}) {
		t.Errorf("Connectors = %v, %v", v, err)
	}

	// Without the lock screen and VPN hooks the calls fail as in Rust.
	for method, want := range map[string]string{
		"Lock":           "lock screen not ready (shell UI not initialized)",
		"VpnSsoCallback": "no VPN browser sign-in is waiting for a callback",
	} {
		args := []any{}
		if method == "VpnSsoCallback" {
			args = append(args, "globalprotectcallback:x")
		}
		err := obj.Call(ServiceName+"."+method, 0, args...).Err
		de, ok := errors.AsType[dbus.Error](err)
		if !ok || de.Name != dbusx.ErrFailed || de.Body[0] != want {
			t.Errorf("%s = %v", method, err)
		}
	}
}

func TestShell1HooksAnswer(t *testing.T) {
	dbustest.SessionBus(t)
	var uri string
	release, err := Serve(dbustest.Conn(t), NewState(nil), Hooks{
		Lock:           func() bool { return true },
		VPNSSOCallback: func(u string) bool { uri = u; return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	obj := dbustest.Conn(t).Object(ServiceName, ServicePath)
	if err := obj.Call(ServiceName+".Lock", 0).Err; err != nil {
		t.Errorf("Lock: %v", err)
	}
	if err := obj.Call(ServiceName+".VpnSsoCallback", 0, "globalprotectcallback:ok").Err; err != nil || uri != "globalprotectcallback:ok" {
		t.Errorf("VpnSsoCallback: %v %q", err, uri)
	}
}

func TestApplicationActions(t *testing.T) {
	dbustest.SessionBus(t)
	conn := dbustest.Conn(t)
	if running, err := IsRunning(conn); err != nil || running {
		t.Fatalf("running before serving: %v %v", running, err)
	}
	quit := make(chan struct{}, 1)
	release, err := ServeApplication(dbustest.Conn(t), func() { quit <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if running, _ := IsRunning(conn); !running {
		t.Fatal("not running after serving")
	}
	obj := conn.Object(AppID, AppPath)
	activate := func(name string) error {
		return obj.Call(ActionsIface+".Activate", 0, name, []dbus.Variant{}, map[string]dbus.Variant{}).Err
	}
	if err := activate(ActionQuit); err != nil {
		t.Fatal(err)
	}
	<-quit
	if err := activate(ActionInspect); err == nil {
		t.Error("the inspector opened in the Go shell")
	}
	var names []string
	if err := obj.Call(ActionsIface+".List", 0).Store(&names); err != nil || !slices.Equal(names, []string{"inspector", "quit"}) {
		t.Errorf("List = %v, %v", names, err)
	}
	if _, err := ServeApplication(dbustest.Conn(t), func() {}); err == nil {
		t.Error("a second shell took the application id")
	}
}
