package screenshot

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a throwaway dbus-daemon and connects to it twice:
// one connection serves, one calls.
func privateBus(t *testing.T) (server, client *dbus.Conn) {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	cmd := exec.Command(bin, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	dial := func() *dbus.Conn {
		conn, err := dbus.Connect(strings.TrimSpace(addr))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	return dial(), dial()
}

func TestDaemonOverTheBus(t *testing.T) {
	server, client := privateBus(t)
	d := &Daemon{host: fakeCapturer{}}
	release, err := d.Export(server)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	c := NewClient(client)
	path, err := c.Capture(context.Background(), "output", "DP-1")
	if err != nil || path != "/p/outputDP-1" {
		t.Fatalf("Capture = %q %v", path, err)
	}
	r, g, b, err := c.PickColor(context.Background())
	if err != nil || r != 0.5 || g != 0.25 || b != 1 {
		t.Fatalf("PickColor = %v %v %v %v", r, g, b, err)
	}

	// A second daemon cannot steal the name.
	if _, err := (&Daemon{host: fakeCapturer{}}).Export(client); err == nil {
		t.Fatal("a second daemon claimed the owned name")
	}

	// Host errors arrive as Failed replies carrying the message.
	d.host = fakeCapturer{err: errors.New("window capture not supported on this compositor")}
	_, err = c.Capture(context.Background(), "window", "")
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.Failed" ||
		dbusErr.Body[0] != "window capture not supported on this compositor" {
		t.Fatalf("Capture error = %#v", err)
	}
}
