package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stubbedev/wayle/internal/dbusx/dbustest"
	"github.com/stubbedev/wayle/service/powerprofiles"
)

// fakeProfiles is power-profiles-daemon as the Go service sees it.
type fakeProfiles struct {
	snap   powerprofiles.Snapshot
	setErr error
}

func (f *fakeProfiles) Read(context.Context) (powerprofiles.Snapshot, error) { return f.snap, nil }

func (f *fakeProfiles) SetActive(_ context.Context, profile string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.snap.Active = profile
	return nil
}

func (f *fakeProfiles) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	return nil, func() {}, nil
}

func servePower(t *testing.T, src *fakeProfiles) {
	t.Helper()
	release, err := powerprofiles.ServeDaemon(dbustest.Conn(t), src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
}

func TestPowerCommandsAgainstTheDaemon(t *testing.T) {
	dbustest.SessionBus(t)
	src := &fakeProfiles{snap: powerprofiles.Snapshot{
		Available: true, Active: "balanced",
		Profiles: []string{"power-saver", "balanced", "performance"},
	}}
	servePower(t, src)

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"power", "status"}, "Active profile: balanced\n"},
		{[]string{"power", "list"}, "Available profiles:\n  power-saver\n  balanced *\n  performance\n"},
		{[]string{"power", "cycle"}, "Profile: performance\n"},
		{[]string{"power", "cycle"}, "Profile: power-saver\n"},
		{[]string{"power", "set", "balanced"}, "Profile set to: balanced\n"},
	}
	for _, c := range cases {
		stdout, stderr, code := runCaptured(t, false, c.args...)
		if code != 0 || stdout != c.want {
			t.Errorf("%v: code %d stdout %q stderr %q, want %q", c.args, code, stdout, stderr, c.want)
		}
	}

	src.snap.PerformanceDegraded = "lap-detected"
	if stdout, _, _ := runCaptured(t, false, "power", "status"); stdout != "Active profile: balanced\nPerformance degraded: lap-detected\n" {
		t.Errorf("degraded status: %q", stdout)
	}
}

func TestPowerErrorsMatchRust(t *testing.T) {
	dbustest.SessionBus(t)

	// No daemon on the bus: the shell is not running.
	_, stderr, code := runCaptured(t, false, "power", "status")
	if code != 1 || stderr != "Error: Power profiles service not running. Start wayle shell first.\n" {
		t.Errorf("not running: code %d %q", code, stderr)
	}

	src := &fakeProfiles{snap: powerprofiles.Snapshot{Available: true, Active: "balanced"}}
	servePower(t, src)

	// An unknown profile is the daemon's InvalidArgs, not a local check.
	_, stderr, code = runCaptured(t, false, "power", "set", "turbo")
	if code != 1 || stderr != "Error: Failed to set profile: Invalid profile: turbo. Expected: power-saver, balanced, performance\n" {
		t.Errorf("invalid profile: code %d %q", code, stderr)
	}
	src.setErr = errors.New("permission denied")
	_, stderr, code = runCaptured(t, false, "power", "cycle")
	if code != 1 || stderr != "Error: Failed to cycle profile: permission denied\n" {
		t.Errorf("set failure: code %d %q", code, stderr)
	}
}

func TestPowerDaemonNeedsTheSystemDaemon(t *testing.T) {
	dbustest.SessionBus(t)
	if _, err := powerprofiles.ServeDaemon(dbustest.Conn(t), &fakeProfiles{}); err == nil {
		t.Fatal("served without power-profiles-daemon")
	}
	_, stderr, _ := runCaptured(t, false, "power", "list")
	if stderr != "Error: Power profiles service not running. Start wayle shell first.\n" {
		t.Errorf("unserved: %q", stderr)
	}
}
