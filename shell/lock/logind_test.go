package lock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeLogindSession is the session object of a fake logind: the real
// path, SetLockedHint, and the LockedHint property.
type fakeLogindSession struct {
	mu     sync.Mutex
	locked bool
}

func (s *fakeLogindSession) SetLockedHint(locked bool) *dbus.Error {
	s.mu.Lock()
	s.locked = locked
	s.mu.Unlock()
	return nil
}

// fakeProps serves org.freedesktop.DBus.Properties.Get for LockedHint.
type fakeProps struct{ s *fakeLogindSession }

func (p fakeProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != logindSessionIf || name != "LockedHint" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	}
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	return dbus.MakeVariant(p.s.locked), nil
}

// fakeManager resolves "auto" to the real session path.
type fakeManager struct{}

const fakeSessionPath = dbus.ObjectPath("/org/freedesktop/login1/session/_31")

func (fakeManager) GetSession(name string) (dbus.ObjectPath, *dbus.Error) {
	if name != "auto" {
		return "", dbus.NewError("org.freedesktop.login1.NoSuchSession", nil)
	}
	return fakeSessionPath, nil
}

func startFakeLogind(t *testing.T) (*dbustest.Bus, *dbus.Conn, *fakeLogindSession) {
	t.Helper()
	bus := dbustest.Start(t)
	server := bus.Conn(t)
	sess := &fakeLogindSession{}
	for _, e := range []struct {
		v    any
		path dbus.ObjectPath
		as   string
	}{
		{fakeManager{}, logindManager, logindManagerIf},
		{sess, fakeSessionPath, logindSessionIf},
		{fakeProps{s: sess}, fakeSessionPath, "org.freedesktop.DBus.Properties"},
	} {
		if err := server.Export(e.v, e.path, e.as); err != nil {
			t.Fatal(err)
		}
	}
	if reply, err := server.RequestName(logindService, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("claim %s: %v %v", logindService, reply, err)
	}
	return bus, server, sess
}

// TestLogindHint: the session resolves through GetSession("auto"), and
// the hint round-trips.
func TestLogindHint(t *testing.T) {
	bus, _, sess := startFakeLogind(t)
	l, err := NewLogind(bus.Conn(t))
	if err != nil {
		t.Fatalf("NewLogind: %v", err)
	}
	if l.Path() != fakeSessionPath {
		t.Errorf("session path = %s, want the real path behind auto", l.Path())
	}
	if locked, err := l.LockedHint(); err != nil || locked {
		t.Fatalf("LockedHint = %v, %v", locked, err)
	}
	if err := l.SetLockedHint(true); err != nil {
		t.Fatal(err)
	}
	sess.mu.Lock()
	got := sess.locked
	sess.mu.Unlock()
	if !got {
		t.Error("SetLockedHint did not reach logind")
	}
	if locked, err := l.LockedHint(); err != nil || !locked {
		t.Errorf("LockedHint after set = %v, %v", locked, err)
	}
}

// TestLogindSignals: Lock and Unlock from the session's path drive the
// callbacks; the same signals from another session do not.
func TestLogindSignals(t *testing.T) {
	bus, server, _ := startFakeLogind(t)
	l, err := NewLogind(bus.Conn(t))
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- l.Listen(ctx, func() { events <- "lock" }, func() { events <- "unlock" })
	}()
	// Let the match rule land before emitting.
	time.Sleep(100 * time.Millisecond)
	emit := func(path dbus.ObjectPath, member string) {
		if err := server.Emit(path, logindSessionIf+"."+member); err != nil {
			t.Fatal(err)
		}
	}
	emit("/org/freedesktop/login1/session/_99", "Lock") // someone else's session
	emit(fakeSessionPath, "Lock")
	emit(fakeSessionPath, "Unlock")
	for _, want := range []string{"lock", "unlock"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event = %s, want %s (another session's signal leaked?)", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event", want)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Listen: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Listen outlived its context")
	}
}

func TestNewLogindWithoutLogind(t *testing.T) {
	bus := dbustest.Start(t)
	if _, err := NewLogind(bus.Conn(t)); err == nil {
		t.Error("NewLogind succeeded with no logind on the bus")
	}
}
