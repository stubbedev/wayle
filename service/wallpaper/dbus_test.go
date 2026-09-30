package wallpaper

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

// privateBus starts a throwaway dbus-daemon and returns its address.
func privateBus(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	addr := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command(bin, "--session", "--nofork", "--nopidfile", "--print-address=1", "--address="+addr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("dbus-daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	return strings.TrimSpace(line)
}

func dial(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestDaemonRoundTrip(t *testing.T) {
	addr := privateBus(t)
	svc := New(Options{Extractor: extract.Config{Tool: extract.None}, Rand: seeded()})
	svc.RegisterMonitor("DP-1")
	svc.RegisterMonitor("DP-2")
	release, err := Export(dial(t, addr), svc)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	client := NewClient(dial(t, addr))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := imageDir(t, "a.png", "b.png")
	if err := client.SetWallpaper(ctx, filepath.Join(dir, "a.png"), "DP-2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := client.WallpaperForMonitor(ctx, "DP-2"); got != filepath.Join(dir, "a.png") {
		t.Errorf("WallpaperForMonitor = %q", got)
	}
	if got, _ := client.WallpaperForMonitor(ctx, ""); got != "" {
		t.Errorf(`WallpaperForMonitor("") = %q, want "" as the Rust daemon answers`, got)
	}
	if err := client.SetFitMode(ctx, "CENTER", "DP-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := client.GetFitMode(ctx, "DP-1"); got != "center" {
		t.Errorf("GetFitMode = %q", got)
	}
	if got, _ := client.GetFitMode(ctx, "nope"); got != "fill" {
		t.Errorf("unknown monitor fit = %q, want the default fill", got)
	}
	// A bad mode is InvalidArgs carrying the parse message.
	var dbusErr dbus.Error
	if err := client.SetFitMode(ctx, "tile", ""); !errors.As(err, &dbusErr) ||
		dbusErr.Name != errInvalidArgs || dbusErr.Body[0] != "Invalid fit mode: tile" {
		t.Errorf("tile = %#v", err)
	}
	if err := client.SetWallpaper(ctx, "/nonexistent.png", ""); !errors.As(err, &dbusErr) ||
		dbusErr.Name != errFailed || dbusErr.Body[0] != "image not found: /nonexistent.png" {
		t.Errorf("missing image = %#v", err)
	}

	if on, _ := client.GetIsCycling(ctx); on {
		t.Error("cycling before start")
	}
	if err := client.StartCycling(ctx, dir, 300, "sequential"); err != nil {
		t.Fatal(err)
	}
	if on, _ := client.GetIsCycling(ctx); !on {
		t.Error("not cycling after start")
	}
	if err := client.StartCycling(ctx, dir, 300, "random"); !errors.As(err, &dbusErr) || dbusErr.Name != errInvalidArgs {
		t.Errorf("bad cycling mode = %#v", err)
	}
	if err := client.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := client.WallpaperForMonitor(ctx, "DP-1"); got != filepath.Join(dir, "b.png") {
		t.Errorf("after Next = %q", got)
	}
	if err := client.Previous(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.StopCycling(ctx); err != nil {
		t.Fatal(err)
	}

	if err := client.SetThemingMonitor(ctx, "DP-2"); err != nil {
		t.Fatal(err)
	}
	obj := client.conn.Object(ServiceName, ServicePath)
	v, err := obj.GetProperty(Interface + ".ThemingMonitor")
	if err != nil || v.Value() != "DP-2" {
		t.Errorf("ThemingMonitor property = %v %v", v, err)
	}
	if v, err := obj.GetProperty(Interface + ".IsCycling"); err != nil || v.Value() != false {
		t.Errorf("IsCycling property = %v %v", v, err)
	}
	if err := obj.SetProperty(Interface+".IsCycling", dbus.MakeVariant(true)); err == nil {
		t.Error("a read-only property accepted a write")
	}

	if err := client.call(ctx, "RegisterMonitor", nil, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	names, _ := client.ListMonitors(ctx)
	if strings.Join(names, ",") != "DP-1,DP-2,HDMI-1" {
		t.Errorf("ListMonitors = %v", names)
	}
	if err := client.call(ctx, "UnregisterMonitor", nil, "HDMI-1"); err != nil {
		t.Fatal(err)
	}
	if names, _ := client.ListMonitors(ctx); len(names) != 2 {
		t.Errorf("after unregister = %v", names)
	}
}

func TestDaemonEmitsColorsExtracted(t *testing.T) {
	addr := privateBus(t)
	svc := New(Options{Extractor: extract.Config{Tool: extract.None}, Rand: seeded()})
	release, err := Export(dial(t, addr), svc)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	listener := dial(t, addr)
	if err := listener.AddMatchSignal(dbus.WithMatchInterface(Interface), dbus.WithMatchMember("ColorsExtracted")); err != nil {
		t.Fatal(err)
	}
	signals := make(chan *dbus.Signal, 4)
	listener.Signal(signals)
	if err := NewClient(dial(t, addr)).ExtractColors(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case sig := <-signals:
		if sig.Path != ServicePath {
			t.Errorf("signal path = %v", sig.Path)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no ColorsExtracted after ExtractColors")
	}
}

func TestExportRefusesAnOwnedName(t *testing.T) {
	addr := privateBus(t)
	release, err := Export(dial(t, addr), New(Options{}))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Export(dial(t, addr), New(Options{})); err == nil {
		t.Error("a second daemon took the owned name")
	}
}
