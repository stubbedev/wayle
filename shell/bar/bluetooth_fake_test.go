package bar

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/bluetooth"
)

// fakeBluetooth is a scripted bluetooth.Source: tests set the state and
// read back the calls the UI made.
type fakeBluetooth struct {
	mu    sync.Mutex
	state bluetooth.State
	calls []string
	// fail makes the named call ("connect", "disconnect", ...) error.
	fail  map[string]error
	ticks chan struct{}
}

func newFakeBluetooth(state bluetooth.State) *fakeBluetooth {
	return &fakeBluetooth{state: state, fail: map[string]error{}, ticks: make(chan struct{}, 1)}
}

func (f *fakeBluetooth) State() bluetooth.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeBluetooth) setState(st bluetooth.State) {
	f.mu.Lock()
	f.state = st
	f.mu.Unlock()
}

func (f *fakeBluetooth) Subscribe() (<-chan struct{}, func()) { return f.ticks, func() {} }

func (f *fakeBluetooth) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := fmt.Sprintf(format, args...)
	f.calls = append(f.calls, call)
	for prefix, err := range f.fail {
		if len(call) >= len(prefix) && call[:len(prefix)] == prefix {
			return err
		}
	}
	return nil
}

func (f *fakeBluetooth) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeBluetooth) Enable(context.Context) error  { return f.record("enable") }
func (f *fakeBluetooth) Disable(context.Context) error { return f.record("disable") }
func (f *fakeBluetooth) StartTimedDiscovery(_ context.Context, d time.Duration) error {
	return f.record("scan %s", d)
}

func (f *fakeBluetooth) Connect(_ context.Context, p dbus.ObjectPath) error {
	return f.record("connect %s", p)
}

func (f *fakeBluetooth) Disconnect(_ context.Context, p dbus.ObjectPath) error {
	return f.record("disconnect %s", p)
}

func (f *fakeBluetooth) Forget(_ context.Context, p dbus.ObjectPath) error {
	return f.record("forget %s", p)
}

func (f *fakeBluetooth) SetTrusted(_ context.Context, p dbus.ObjectPath, trusted bool) error {
	return f.record("trust %s %v", p, trusted)
}

func (f *fakeBluetooth) TrustPaired(_ context.Context, p dbus.ObjectPath) error {
	return f.record("trust-paired %s", p)
}

// answer records a pairing answer; an accepted one clears the pending
// request, as the service does.
func (f *fakeBluetooth) answer(format string, args ...any) error {
	if err := f.record(format, args...); err != nil {
		return err
	}
	f.mu.Lock()
	f.state.Pairing = nil
	f.mu.Unlock()
	return nil
}

func (f *fakeBluetooth) ProvidePin(pin string) error { return f.answer("pin %s", pin) }

func (f *fakeBluetooth) ProvidePasskey(passkey uint32) error {
	return f.answer("passkey %d", passkey)
}

func (f *fakeBluetooth) ProvideConfirmation(ok bool) error {
	return f.answer("confirmation %v", ok)
}

func (f *fakeBluetooth) ProvideAuthorization(ok bool) error {
	return f.answer("authorization %v", ok)
}

func (f *fakeBluetooth) ProvideServiceAuthorization(ok bool) error {
	return f.answer("service-authorization %v", ok)
}

func (f *fakeBluetooth) CancelPendingRequest() { _ = f.answer("cancel") }
