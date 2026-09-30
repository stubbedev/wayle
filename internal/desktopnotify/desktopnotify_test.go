package desktopnotify

import (
	"context"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

type fakeDaemon struct {
	mu  sync.Mutex
	got []string
}

func (f *fakeDaemon) Notify(appName string, _ uint32, appIcon, summary, body string, _ []string, _ map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	if timeout != -1 {
		return 0, dbus.MakeFailedError(nil)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, appName, appIcon, summary, body)
	return 7, nil
}

func TestSendDeliversToTheDaemon(t *testing.T) {
	bus := dbustest.Start(t)
	daemonConn := bus.Conn(t)
	daemon := &fakeDaemon{}
	if err := daemonConn.Export(daemon, path, busName); err != nil {
		t.Fatal(err)
	}
	if _, err := daemonConn.RequestName(busName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}

	id, err := NewSender(bus.Conn(t)).Send(context.Background(), "Wayle", "Alice", "Lunch?", "ld-mail-symbolic")
	if err != nil {
		t.Fatal(err)
	}
	if id != 7 {
		t.Errorf("id = %d, want the daemon's 7", id)
	}
	want := []string{"Wayle", "ld-mail-symbolic", "Alice", "Lunch?"}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for i, w := range want {
		if i >= len(daemon.got) || daemon.got[i] != w {
			t.Fatalf("daemon got %q, want %q", daemon.got, want)
		}
	}
}

func TestSendWithoutADaemonErrors(t *testing.T) {
	bus := dbustest.Start(t)
	if _, err := NewSender(bus.Conn(t)).Send(context.Background(), "Wayle", "s", "b", ""); err == nil {
		t.Error("no daemon on the bus: want an error")
	}
}
