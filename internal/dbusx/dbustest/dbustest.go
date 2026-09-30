// Package dbustest runs a private session bus for tests: a real
// dbus-daemon on a socket in the test's temp dir, exported as
// DBUS_SESSION_BUS_ADDRESS for the test's duration, so daemon and
// client halves talk exactly as they do on a desktop.
package dbustest

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// SessionBus starts the private bus and points the session-bus
// environment at it; the daemon stops with the test. The .#go dev
// shell provides dbus-daemon.
func SessionBus(t *testing.T) {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Fatalf("dbus-daemon not found (run inside `nix develop .#go`): %v", err)
	}
	socket := filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address", "--address=unix:path="+socket) //nolint:gosec // dbus-daemon from PATH on a socket in the test temp dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon address: %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(line))
}

// Conn is a fresh connection to the private bus, closed with the test.
func Conn(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatalf("connect private bus: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
