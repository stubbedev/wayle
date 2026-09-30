package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stubbedev/wayle/internal/dbusx/dbustest"
	"github.com/stubbedev/wayle/service/shellipc"
)

// fakeShell owns the application id; quit releases it the way the
// process exiting would.
func fakeShell(t *testing.T) {
	t.Helper()
	conn := dbustest.Conn(t)
	var release func()
	var err error
	release, err = shellipc.ServeApplication(conn, func() { go release() })
	if err != nil {
		t.Fatal(err)
	}
}

func TestPanelLifecycleCommands(t *testing.T) {
	dbustest.SessionBus(t)
	if stdout, _, code := runCaptured(t, false, "panel", "status"); code != 0 || stdout != "Panel is not running\n" {
		t.Errorf("status down: %d %q", code, stdout)
	}
	for _, cmd := range []string{"stop", "inspect"} {
		if _, stderr, code := runCaptured(t, false, "panel", cmd); code != 1 || stderr != "Error: Panel is not running\n" {
			t.Errorf("%s down: %d %q", cmd, code, stderr)
		}
	}

	fakeShell(t)
	if stdout, _, _ := runCaptured(t, false, "panel", "status"); stdout != "Panel is running\n" {
		t.Errorf("status up: %q", stdout)
	}
	if stdout, _, code := runCaptured(t, false, "panel", "start"); code != 0 || stdout != "Panel is already running\n" {
		t.Errorf("start while running: %d %q", code, stdout)
	}
	if _, stderr, code := runCaptured(t, false, "panel", "inspect"); code != 1 ||
		stderr != "Error: Failed to open inspector: org.freedesktop.DBus.Error.Failed: the GTK inspector does not exist in the Go shell\n" {
		t.Errorf("inspect: %d %q", code, stderr)
	}
	if stdout, stderr, code := runCaptured(t, false, "panel", "stop"); code != 0 || stdout != "Panel stopped\n" {
		t.Errorf("stop: %d %q %q", code, stdout, stderr)
	}
	if stdout, _, _ := runCaptured(t, false, "panel", "status"); stdout != "Panel is not running\n" {
		t.Errorf("status after stop: %q", stdout)
	}
}

func TestPanelBarVisibility(t *testing.T) {
	dbustest.SessionBus(t)
	if _, stderr, code := runCaptured(t, false, "panel", "hide"); code != 1 || stderr != "Error: Shell service not running. Start wayle shell first.\n" {
		t.Errorf("shell down: %d %q", code, stderr)
	}
	state := shellipc.NewState(nil)
	state.SetConnectors([]string{"DP-1", "DP-2"})
	release, err := shellipc.Serve(dbustest.Conn(t), state, shellipc.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"panel", "hide", "DP-1"}, "Bar hidden on DP-1\n"},
		{[]string{"panel", "toggle"}, "All bars toggled\n"},
		{[]string{"panel", "hide"}, "All bars hidden\n"},
		{[]string{"panel", "show", "DP-2"}, "Bar shown on DP-2\n"},
		{[]string{"panel", "toggle", "DP-2"}, "Bar toggled on DP-2\n"},
		{[]string{"panel", "show"}, "All bars shown\n"},
	}
	wantHidden := []map[string]bool{
		{"DP-1": true}, {}, {"DP-1": true, "DP-2": true}, {"DP-1": true}, {"DP-1": true, "DP-2": true}, {},
	}
	for i, s := range steps {
		stdout, stderr, code := runCaptured(t, false, s.args...)
		if code != 0 || stdout != s.want {
			t.Errorf("%v: %d %q %q", s.args, code, stdout, stderr)
		}
		for _, c := range []string{"DP-1", "DP-2"} {
			if state.Hidden(c) != wantHidden[i][c] {
				t.Errorf("after %v: %s hidden = %v", s.args, c, state.Hidden(c))
			}
		}
	}
}

func TestShellRefusesASecondInstance(t *testing.T) {
	dbustest.SessionBus(t)
	started := 0
	start := func() error { started++; return nil }
	var stderr bytes.Buffer
	if err := runShell(&stderr, start); err != nil || started != 1 || stderr.Len() != 0 {
		t.Fatalf("first shell: %v started=%d %q", err, started, stderr.String())
	}
	fakeShell(t)
	if err := runShell(&stderr, start); err != nil || started != 1 || stderr.String() != "Wayle shell is already running\n" {
		t.Fatalf("second shell: %v started=%d %q", err, started, stderr.String())
	}
	// A start failure is the shell's error.
	stderr.Reset()
	dbustest.SessionBus(t)
	if err := runShell(&stderr, func() error { return errors.New("no output") }); err == nil {
		t.Error("start error swallowed")
	}
}
