package portal

import (
	"os"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Inhibit flags of the portal spec.
const (
	inhibitLogout  = 1
	inhibitSuspend = 4
	inhibitIdle    = 8
)

// inhibitor is org.freedesktop.impl.portal.Inhibit (inhibit.rs): a
// logind block lock for the request's lifetime, held by a Request at
// the handle whose Close drops the lock's fd.
type inhibitor struct {
	conn *dbus.Conn
	// lock takes a logind lock for what ("idle:sleep:shutdown").
	lock func(what string) (*os.File, error)
}

func (i inhibitor) iface() dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Inhibit",
		Methods:    i,
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// Inhibit takes the lock and exports its Request; nothing to inhibit,
// or a logind failure, leaves the request without one.
func (i inhibitor) Inhibit(handle dbus.ObjectPath, _, _ string, flags uint32, _ Vardict) *dbus.Error {
	what := inhibitWhat(flags)
	if what == "" {
		return nil
	}
	fd, err := i.lock(what)
	if err != nil {
		warnf("inhibit: %v", err)
		return nil
	}
	l := &inhibitLock{fd: fd}
	unexport, err := dbusx.Export(i.conn, handle, dbusx.Interface{Name: RequestIface, Methods: l})
	if err != nil {
		warnf("inhibit: cannot export request object: %v", err)
		_ = fd.Close()
		return nil
	}
	l.unexport = unexport
	return nil
}

// inhibitLock is the Request holding one lock.
type inhibitLock struct {
	once     sync.Once
	fd       *os.File
	unexport func()
}

// Close releases the inhibition.
func (l *inhibitLock) Close() *dbus.Error {
	l.once.Do(func() {
		l.unexport()
		_ = l.fd.Close()
	})
	return nil
}

// inhibitWhat is logind's what for the flags.
func inhibitWhat(flags uint32) string {
	var what []string
	if flags&inhibitIdle != 0 {
		what = append(what, "idle")
	}
	if flags&inhibitSuspend != 0 {
		what = append(what, "sleep")
	}
	if flags&inhibitLogout != 0 {
		what = append(what, "shutdown")
	}
	return strings.Join(what, ":")
}

// logindLock takes a block inhibitor from logind on the system bus.
func logindLock(what string) (*os.File, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	var fd dbus.UnixFD
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		Call("org.freedesktop.login1.Manager.Inhibit", 0, what, "Wayle portal", "Application requested", "block").Store(&fd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "inhibit"), nil
}
