package powerprofiles

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Wayle's own interface over the service, which `wayle power` drives
// (wayle-power-profiles/src/dbus).
const (
	ServiceName = "com.wayle.PowerProfiles1"
	ServicePath = dbus.ObjectPath("/com/wayle/PowerProfiles")
)

// profileName is PowerProfile's From<&str> + Display: the three known
// names, anything else "unknown".
func profileName(raw string) string {
	switch raw {
	case ProfilePowerSaver, ProfileBalanced, ProfilePerformance:
		return raw
	default:
		return "unknown"
	}
}

// degradedName is PerformanceDegradationReason's round trip.
func degradedName(raw string) string {
	switch raw {
	case "", "lap-detected", "high-operating-temperature":
		return raw
	default:
		return "unknown"
	}
}

// cycleNext is dbus/server.rs's cycle: the fixed three-step order, an
// unknown profile going to balanced (unlike NextProfile it ignores
// which profiles the hardware offers).
func cycleNext(current string) string {
	switch current {
	case ProfilePowerSaver:
		return ProfileBalanced
	case ProfileBalanced:
		return ProfilePerformance
	case ProfilePerformance:
		return ProfilePowerSaver
	default:
		return ProfileBalanced
	}
}

// Daemon is the com.wayle.PowerProfiles1 object (PowerProfilesDaemon).
type Daemon struct {
	src Source
}

const daemonTimeout = 5 * time.Second

func (d *Daemon) read() Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), daemonTimeout)
	defer cancel()
	snap, _ := d.src.Read(ctx)
	return snap
}

// SetProfile switches to a named profile.
func (d *Daemon) SetProfile(profile string) *dbus.Error {
	switch profile {
	case ProfilePowerSaver, ProfileBalanced, ProfilePerformance:
	default:
		return dbusx.InvalidArgs("Invalid profile: " + profile + ". Expected: power-saver, balanced, performance")
	}
	return d.set(profile)
}

// Cycle advances to the next profile.
func (d *Daemon) Cycle() *dbus.Error {
	return d.set(cycleNext(profileName(d.read().Active)))
}

func (d *Daemon) set(profile string) *dbus.Error {
	ctx, cancel := context.WithTimeout(context.Background(), daemonTimeout)
	defer cancel()
	if err := d.src.SetActive(ctx, profile); err != nil {
		return dbusx.Failed(err.Error())
	}
	return nil
}

// ListProfiles names the available profiles.
func (d *Daemon) ListProfiles() ([]string, *dbus.Error) {
	profiles := d.read().Profiles
	out := make([]string, len(profiles))
	for i, p := range profiles {
		out[i] = profileName(p)
	}
	return out, nil
}

// ServeDaemon exports the interface for the service over src. Like the
// Rust builder it is only served when power-profiles-daemon answered:
// without it the CLI reports the service as not running.
func ServeDaemon(conn *dbus.Conn, src Source) (func(), error) {
	d := &Daemon{src: src}
	if !d.read().Available {
		return nil, errUnavailable
	}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      ServiceName,
		Path:      ServicePath,
		Interface: ServiceName,
		Methods:   d,
		Properties: dbusx.Getters{
			"ActiveProfile":       func() any { return profileName(d.read().Active) },
			"PerformanceDegraded": func() any { return degradedName(d.read().PerformanceDegraded) },
			"ProfileCount":        func() any { return uint32(len(d.read().Profiles)) },
		},
	})
}

var errUnavailable = errors.New("powerprofiles: power-profiles-daemon unavailable")
