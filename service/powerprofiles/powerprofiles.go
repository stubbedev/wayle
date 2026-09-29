// Package powerprofiles is the power-profiles-daemon client: the
// active profile, the available set, property-change ticks, and the
// cycle action.
package powerprofiles

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Profile names as the daemon spells them.
const (
	ProfilePowerSaver  = "power-saver"
	ProfileBalanced    = "balanced"
	ProfilePerformance = "performance"
)

// Canonical cycle order (helpers.rs): power-saver → balanced →
// performance.
var order = []string{ProfilePowerSaver, ProfileBalanced, ProfilePerformance}

// NextProfile is helpers.rs's next_profile: the successor of current
// in the canonical order restricted to available (empty available
// means all three), falling back to balanced.
func NextProfile(current string, available []string) string {
	cycle := order
	if len(available) > 0 {
		cycle = make([]string, 0, len(available))
		for _, candidate := range order {
			if slices.Contains(available, candidate) {
				cycle = append(cycle, candidate)
			}
		}
	}
	if len(cycle) == 0 {
		return ProfileBalanced
	}
	for i, candidate := range cycle {
		if candidate == current {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return cycle[0]
}

// Snapshot is one daemon state.
type Snapshot struct {
	Available bool
	Active    string
	Profiles  []string
}

const (
	daemonName  = "org.freedesktop.UPower.PowerProfiles"
	daemonPath  = "/org/freedesktop/UPower/PowerProfiles"
	daemonIface = "org.freedesktop.UPower.PowerProfiles"
	properties  = "org.freedesktop.DBus.Properties"
)

// Source is the module's seam.
type Source interface {
	// Read collects the snapshot.
	Read(ctx context.Context) (Snapshot, error)
	// SetActive switches the profile.
	SetActive(ctx context.Context, profile string) error
	// Subscribe ticks on daemon property changes. The channel closes
	// when ctx completes; stop terminates the listener.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

// System reads the real daemon over the system bus.
type System struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

// NewSystem connects to the system bus.
func NewSystem() (*System, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("powerprofiles: system bus: %w", err)
	}
	return &System{conn: conn}, nil
}

// Close drops the bus connection.
func (s *System) Close() error { return s.conn.Close() }

// Read collects the snapshot; an absent daemon reads as unavailable
// with the canonical cycle available.
func (s *System) Read(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := Snapshot{Active: ProfileBalanced}
	obj := s.conn.Object(daemonName, daemonPath)
	var active string
	if err := obj.CallWithContext(ctx, properties+".Get", 0, daemonIface, "ActiveProfile").Store(&active); err != nil {
		return snap, nil
	}
	snap.Available = true
	snap.Active = active

	var raw []map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, properties+".Get", 0, daemonIface, "Profiles").Store(&raw); err == nil {
		for _, entry := range raw {
			if name, ok := entry["Profile"].Value().(string); ok {
				snap.Profiles = append(snap.Profiles, name)
			}
		}
	}
	return snap, nil
}

// SetActive writes the ActiveProfile property.
func (s *System) SetActive(ctx context.Context, profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj := s.conn.Object(daemonName, daemonPath)
	return obj.CallWithContext(ctx, properties+".Set", 0, daemonIface, "ActiveProfile",
		dbus.MakeVariant(profile)).Err
}

// Subscribe matches the daemon's PropertiesChanged.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := s.conn.AddMatchSignal(
		dbus.WithMatchInterface(properties),
		dbus.WithMatchArg(0, daemonIface),
		dbus.WithMatchSender(daemonName),
	); err != nil {
		return nil, nil, fmt.Errorf("powerprofiles: match properties: %w", err)
	}
	ticks := make(chan struct{}, 1)
	signals := make(chan *dbus.Signal, 8)
	s.conn.Signal(signals)
	go func() {
		defer close(ticks)
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if sig.Name != properties+".PropertiesChanged" {
					continue
				}
				select {
				case ticks <- struct{}{}:
				default:
				}
			}
		}
	}()
	stop := func() { s.conn.RemoveSignal(signals) }
	return ticks, stop, nil
}
