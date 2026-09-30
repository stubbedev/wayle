// Package shellipc is the shell's own session-bus surface: the
// com.wayle.Shell1 control interface (wayle-shell-core's shell_ipc:
// bar visibility per monitor, the lock and VPN sign-in hooks) and the
// GApplication identity com.wayle.shell with its org.gtk.Actions
// (quit, inspector), which `wayle panel` and single-instance checks use.
package shellipc

import (
	"slices"
	"sort"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// com.wayle.Shell1 (wayle-ipc/src/shell_ipc.rs).
const (
	ServiceName = "com.wayle.Shell1"
	ServicePath = dbus.ObjectPath("/com/wayle/Shell")
)

// State is ShellIpcState: the connectors that have a bar and the
// hidden subset. Changes reach OnHidden with the new hidden set.
type State struct {
	mu         sync.Mutex
	connectors []string
	hidden     map[string]bool
	onHidden   func(hidden map[string]bool)
}

// NewState builds an empty state; onHidden (may be nil) observes every
// change of the hidden set.
func NewState(onHidden func(hidden map[string]bool)) *State {
	return &State{hidden: map[string]bool{}, onHidden: onHidden}
}

// SetConnectors is monitors.rs's sync_ipc_state: the bars that exist,
// with hidden entries for vanished connectors dropped.
func (s *State) SetConnectors(connectors []string) {
	s.mu.Lock()
	s.connectors = slices.Clone(connectors)
	changed := false
	for name := range s.hidden {
		if !slices.Contains(connectors, name) {
			delete(s.hidden, name)
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

// Hidden reports whether connector's bar is hidden.
func (s *State) Hidden(connector string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hidden[connector]
}

func (s *State) notify() {
	if s.onHidden == nil {
		return
	}
	s.mu.Lock()
	snapshot := make(map[string]bool, len(s.hidden))
	for k := range s.hidden {
		snapshot[k] = true
	}
	s.mu.Unlock()
	s.onHidden(snapshot)
}

// update applies fn under the lock and notifies when it reports a
// change.
func (s *State) update(fn func() bool) {
	s.mu.Lock()
	changed := fn()
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

// Hide is bar.rs's hide: "" hides every bar; an unknown connector is
// ignored.
func (s *State) Hide(monitor string) {
	s.update(func() bool {
		switch {
		case monitor == "":
			for _, c := range s.connectors {
				s.hidden[c] = true
			}
		case slices.Contains(s.connectors, monitor):
			s.hidden[monitor] = true
		default:
			return false
		}
		return true
	})
}

// Show is bar.rs's show: "" shows every bar.
func (s *State) Show(monitor string) {
	s.update(func() bool {
		switch {
		case monitor == "":
			s.hidden = map[string]bool{}
		case slices.Contains(s.connectors, monitor):
			delete(s.hidden, monitor)
		default:
			return false
		}
		return true
	})
}

// Toggle is bar.rs's toggle: "" hides all when none is hidden and
// shows all otherwise.
func (s *State) Toggle(monitor string) {
	s.update(func() bool {
		switch {
		case monitor == "":
			if len(s.hidden) == 0 {
				for _, c := range s.connectors {
					s.hidden[c] = true
				}
			} else {
				s.hidden = map[string]bool{}
			}
		case slices.Contains(s.connectors, monitor):
			if s.hidden[monitor] {
				delete(s.hidden, monitor)
			} else {
				s.hidden[monitor] = true
			}
		default:
			return false
		}
		return true
	})
}

func (s *State) hiddenSorted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.hidden))
	for name := range s.hidden {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *State) connectorList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.connectors)
}

// Hooks are the actions other parts of the shell provide. A nil hook
// answers as the Rust shell does before its component exists.
type Hooks struct {
	// Lock shows the lock screen; false means it is not ready.
	Lock func() bool
	// VPNSSOCallback hands a browser callback to a waiting sign-in;
	// false means none is waiting.
	VPNSSOCallback func(uri string) bool
}

// daemon is the com.wayle.Shell1 object (ShellIpcDaemon).
type daemon struct {
	state *State
	hooks Hooks
}

// BarHide hides a bar ("" for all).
func (d *daemon) BarHide(monitor string) *dbus.Error {
	d.state.Hide(monitor)
	return nil
}

// BarShow shows a bar ("" for all).
func (d *daemon) BarShow(monitor string) *dbus.Error {
	d.state.Show(monitor)
	return nil
}

// BarToggle toggles a bar ("" for all).
func (d *daemon) BarToggle(monitor string) *dbus.Error {
	d.state.Toggle(monitor)
	return nil
}

// Lock asks for the lock screen.
func (d *daemon) Lock() *dbus.Error {
	if d.hooks.Lock != nil && d.hooks.Lock() {
		return nil
	}
	return dbusx.Failed("lock screen not ready (shell UI not initialized)")
}

// VpnSsoCallback delivers a browser sign-in callback.
func (d *daemon) VpnSsoCallback(uri string) *dbus.Error {
	if d.hooks.VPNSSOCallback != nil && d.hooks.VPNSSOCallback(uri) {
		return nil
	}
	return dbusx.Failed("no VPN browser sign-in is waiting for a callback")
}

// Serve exports com.wayle.Shell1 over state.
func Serve(conn *dbus.Conn, state *State, hooks Hooks) (func(), error) {
	d := &daemon{state: state, hooks: hooks}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      ServiceName,
		Path:      ServicePath,
		Interface: ServiceName,
		Methods:   d,
		Properties: dbusx.Getters{
			"BarHidden":  func() any { return state.hiddenSorted() },
			"Connectors": func() any { return state.connectorList() },
		},
	})
}
