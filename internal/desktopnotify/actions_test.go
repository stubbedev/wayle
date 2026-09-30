package desktopnotify

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// askDaemon answers Notify with id 9 and then plays its script of
// signals, as a user clicking (or ignoring) the buttons would.
type askDaemon struct {
	conn   *dbus.Conn
	script [][]any // each: signal member, then its body

	mu      sync.Mutex
	actions []string
	hints   map[string]dbus.Variant
	timeout int32
}

func (d *askDaemon) Notify(_ string, _ uint32, _, _, _ string, actions []string, hints map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	d.mu.Lock()
	d.actions, d.hints, d.timeout = actions, hints, timeout
	d.mu.Unlock()
	go func() {
		time.Sleep(20 * time.Millisecond)
		for _, sig := range d.script {
			_ = d.conn.Emit(path, busName+"."+sig[0].(string), sig[1:]...)
		}
	}()
	return 9, nil
}

func startAskDaemon(t *testing.T, script ...[]any) (*dbustest.Bus, *askDaemon) {
	t.Helper()
	bus := dbustest.Start(t)
	conn := bus.Conn(t)
	d := &askDaemon{conn: conn, script: script}
	if err := conn.Export(d, path, busName); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return bus, d
}

var yesNoActions = []Action{{Key: "deny", Label: "Deny"}, {Key: "allow", Label: "Allow"}}

func TestAskReportsThePressedButton(t *testing.T) {
	bus, d := startAskDaemon(t,
		[]any{"ActionInvoked", uint32(3), "deny"}, // another notification's click
		[]any{"ActionInvoked", uint32(9), "allow"},
	)
	key, ok, err := NewSender(bus.Conn(t)).Ask(context.Background(), "Wayle", "s", "b", "ld-bluetooth-symbolic", yesNoActions, time.Minute)
	if err != nil || !ok || key != "allow" {
		t.Fatalf("Ask = %q %v %v, want allow", key, ok, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !slices.Equal(d.actions, []string{"deny", "Deny", "allow", "Allow"}) {
		t.Errorf("actions on the wire = %v", d.actions)
	}
	if d.hints["image-path"].Value() != "ld-bluetooth-symbolic" {
		t.Errorf("hints = %v, want the icon as image-path", d.hints)
	}
	if d.timeout != 60000 {
		t.Errorf("timeout = %d, want 60000", d.timeout)
	}
}

func TestAskClosedWithoutAnAnswer(t *testing.T) {
	bus, _ := startAskDaemon(t, []any{"NotificationClosed", uint32(9), uint32(1)})
	key, ok, err := NewSender(bus.Conn(t)).Ask(context.Background(), "Wayle", "s", "b", "", yesNoActions, time.Minute)
	if err != nil || ok || key != "" {
		t.Fatalf("Ask = %q %v %v, want no answer", key, ok, err)
	}
}

func TestAskGivesUpWithTheContext(t *testing.T) {
	bus, _ := startAskDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok, err := NewSender(bus.Conn(t)).Ask(ctx, "Wayle", "s", "b", "", yesNoActions, time.Minute); ok || err != nil {
		t.Fatalf("Ask = %v %v, want no answer on timeout", ok, err)
	}
}

func TestAskWithoutADaemonErrors(t *testing.T) {
	bus := dbustest.Start(t)
	if _, _, err := NewSender(bus.Conn(t)).Ask(context.Background(), "Wayle", "s", "b", "", yesNoActions, time.Minute); err == nil {
		t.Error("no daemon: want an error")
	}
}
