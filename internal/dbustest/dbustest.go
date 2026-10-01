// Package dbustest starts a private dbus-daemon for tests, so services
// that export or call D-Bus objects are exercised over a real bus
// without touching the user's session or system bus.
package dbustest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// config is a session-bus policy that lets every connection own any
// name and talk to anyone, like the stock session.conf.
const config = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:dir=%DIR%</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// Bus is one private bus.
type Bus struct {
	// Address is the bus address, for Dial or DBUS_SESSION_BUS_ADDRESS.
	Address string
}

// Start launches a dbus-daemon for the test and stops it at cleanup.
// A missing dbus-daemon fails the test: the .#go devShell provides one.
func Start(t *testing.T) *Bus {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Fatalf("dbustest: dbus-daemon not on PATH: %v", err)
	}
	// The socket lives in a short directory of its own: t.TempDir()
	// embeds the test name, and a long one overflows sun_path's 108
	// bytes, which dbus-daemon answers by exiting.
	sockDir, err := os.MkdirTemp("", "dbt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	cfgPath := filepath.Join(t.TempDir(), "bus.conf")
	if err := os.WriteFile(cfgPath, []byte(strings.ReplaceAll(config, "%DIR%", sockDir)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(daemon, "--config-file="+cfgPath, "--nofork", "--print-address=1") //nolint:gosec // the daemon path comes from PATH lookup in a test
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("dbustest: start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	select {
	case addr, ok := <-lines:
		if !ok || addr == "" {
			t.Fatal("dbustest: dbus-daemon printed no address")
		}
		return &Bus{Address: strings.TrimSpace(addr)}
	case <-time.After(5 * time.Second):
		t.Fatal("dbustest: dbus-daemon did not start")
	}
	return nil
}

// Conn opens a new authenticated connection to the bus, closed at
// cleanup. Each call is a distinct peer with its own unique name.
func (b *Bus) Conn(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(b.Address)
	if err != nil {
		t.Fatalf("dbustest: connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// UseAsSessionBus points DBUS_SESSION_BUS_ADDRESS at the bus for the
// test, for code that dials the session bus itself.
func (b *Bus) UseAsSessionBus(t *testing.T) {
	t.Helper()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", b.Address)
}

// UseAsSystemBus points DBUS_SYSTEM_BUS_ADDRESS at the bus for the
// test, for code that dials the system bus itself.
func (b *Bus) UseAsSystemBus(t *testing.T) {
	t.Helper()
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", b.Address)
}

// Session starts a private bus and makes it the test's session bus,
// for daemon and CLI halves that both dial the session bus.
func Session(t *testing.T) *Bus {
	t.Helper()
	b := Start(t)
	b.UseAsSessionBus(t)
	return b
}

// SessionConn opens a connection to the session bus the test pointed
// at a private bus (Session or UseAsSessionBus), closed at cleanup.
func SessionConn(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatalf("dbustest: connect session bus: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
