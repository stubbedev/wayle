package hyprsunset

import (
	"context"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeSocket answers hyprsunset's two queries like the real daemon.
func fakeSocket(t *testing.T, replies map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".hyprsunset.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			cmd, _ := io.ReadAll(conn)
			_, _ = conn.Write([]byte(replies[string(cmd)]))
			_ = conn.Close()
		}
	}()
	return path
}

func TestQueryState(t *testing.T) {
	sock := fakeSocket(t, map[string]string{"temperature": "4499.6\n", "gamma": "80"})
	if st, ok := QueryState(sock); !ok || st.Temp != 4500 || st.Gamma != 80 {
		t.Errorf("state = %+v, %v; want the rounded 4500K at 80%%", st, ok)
	}
	// No daemon: off.
	if _, ok := QueryState(filepath.Join(t.TempDir(), "none.sock")); ok {
		t.Error("a missing socket reported a running filter")
	}
	// A garbage reply is not a state.
	bad := fakeSocket(t, map[string]string{"temperature": "err", "gamma": "80"})
	if _, ok := QueryState(bad); ok {
		t.Error("an unparsable reply reported a state")
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "abc")
	if p, ok := SocketPath(); !ok || p != "/run/user/1000/hypr/abc/.hyprsunset.sock" {
		t.Errorf("path = %q, %v", p, ok)
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if _, ok := SocketPath(); ok {
		t.Error("a path without the instance signature")
	}
}

func TestFilterTracksOneChild(t *testing.T) {
	var args [][2]int
	f := &Filter{command: func(temp, gamma int) *exec.Cmd {
		args = append(args, [2]int{temp, gamma})
		return exec.Command("sleep", "30")
	}}
	if err := f.Start(4500, 90); err != nil {
		t.Skipf("no sleep binary: %v", err)
	}
	first := f.child
	if err := f.Start(3000, 80); err != nil {
		t.Fatal(err)
	}
	// Replacing terminates the previous child.
	done := make(chan struct{})
	go func() { _, _ = first.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("the replaced child was not terminated")
	}
	if len(args) != 2 || args[1] != [2]int{3000, 80} {
		t.Errorf("spawn args = %v", args)
	}
	if err := f.Stop(); err != nil || f.Running() {
		t.Errorf("stop: %v, running %v", err, f.Running())
	}
	// Stopping with nothing running is a no-op.
	if err := f.Stop(); err != nil {
		t.Error(err)
	}
}

// fakeGeoClue serves Manager.GetClient, a Client that emits
// LocationUpdated on Start, and one Location.
type fakeGeoClue struct {
	conn *dbus.Conn
}

func (g *fakeGeoClue) GetClient() (dbus.ObjectPath, *dbus.Error) {
	return "/org/freedesktop/GeoClue2/Client/1", nil
}

type fakeGeoClient struct{ g *fakeGeoClue }

func (c *fakeGeoClient) Start() *dbus.Error {
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = c.g.conn.Emit("/org/freedesktop/GeoClue2/Client/1", geoclueClient+".LocationUpdated",
			dbus.ObjectPath("/"), dbus.ObjectPath("/org/freedesktop/GeoClue2/Location/1"))
	}()
	return nil
}

func (c *fakeGeoClient) Stop() *dbus.Error { return nil }

func TestQueryLocationOverGeoClue(t *testing.T) {
	bus := dbustest.Start(t)
	daemon := bus.Conn(t)
	g := &fakeGeoClue{conn: daemon}
	if err := daemon.Export(g, geoclueManagerPath, geoclueManager); err != nil {
		t.Fatal(err)
	}
	clientPath := dbus.ObjectPath("/org/freedesktop/GeoClue2/Client/1")
	if err := daemon.Export(&fakeGeoClient{g: g}, clientPath, geoclueClient); err != nil {
		t.Fatal(err)
	}
	clientProps, err := prop.Export(daemon, clientPath, prop.Map{geoclueClient: {
		"DesktopId":              {Value: "", Writable: true, Emit: prop.EmitFalse},
		"RequestedAccuracyLevel": {Value: uint32(0), Writable: true, Emit: prop.EmitFalse},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prop.Export(daemon, "/org/freedesktop/GeoClue2/Location/1", prop.Map{geoclueLocation: {
		"Latitude":  {Value: 55.6761, Emit: prop.EmitConst},
		"Longitude": {Value: 12.5683, Emit: prop.EmitConst},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.RequestName(geoclueService, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}

	loc, err := QueryLocation(context.Background(), bus.Conn(t))
	if err != nil {
		t.Fatalf("QueryLocation: %v", err)
	}
	if loc.Latitude != 55.6761 || loc.Longitude != 12.5683 {
		t.Errorf("location = %+v", loc)
	}
	// The client was configured before Start.
	if id, _ := clientProps.Get(geoclueClient, "DesktopId"); id.Value() != "wayle" {
		t.Errorf("DesktopId = %v, want wayle", id.Value())
	}
	if lvl, _ := clientProps.Get(geoclueClient, "RequestedAccuracyLevel"); lvl.Value() != accuracyCity {
		t.Errorf("accuracy = %v, want city", lvl.Value())
	}
}

func TestQueryLocationFailsWithoutADaemon(t *testing.T) {
	bus := dbustest.Start(t)
	if _, err := queryLocation(context.Background(), bus.Conn(t), 100*time.Millisecond); err == nil {
		t.Error("no GeoClue on the bus: want an error")
	}
}
