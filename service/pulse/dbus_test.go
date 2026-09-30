package pulse

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/pulse/native"
)

// daemonFixture exports a daemon for a fixture service on a private
// bus and returns a CLI-side client on another connection.
func daemonFixture(t *testing.T) (*Client, *Service, *dbustest.Bus) {
	t.Helper()
	srv := fixture(t)
	svc := connect(t, srv)
	bus := dbustest.Start(t)
	release, err := NewDaemon(svc).Export(bus.Conn(t))
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	t.Cleanup(release)
	return NewClient(bus.Conn(t)), svc, bus
}

func dbusFailure(t *testing.T, err error) string {
	t.Helper()
	var derr dbus.Error
	if !errors.As(err, &derr) {
		t.Fatalf("err = %v (%T), want a dbus.Error", err, err)
	}
	if derr.Name != "org.freedesktop.DBus.Error.Failed" {
		t.Errorf("error name = %s, want Failed", derr.Name)
	}
	return derr.Error()
}

func TestDaemonVolumeMethods(t *testing.T) {
	c, svc, _ := daemonFixture(t)
	ctx := context.Background()

	got, err := c.SetOutputVolume(ctx, 130)
	if err != nil || got != 100 {
		t.Fatalf("SetOutputVolume(130) = %v, %v, want clamped 100", got, err)
	}
	waitFor(t, "100%", func() bool { d, _ := svc.DefaultOutput(); return d.Volume.IsNormal() })

	got, err = c.AdjustOutputVolume(ctx, -25)
	if err != nil || got != 75 {
		t.Fatalf("AdjustOutputVolume(-25) = %v, %v", got, err)
	}
	waitFor(t, "75%", func() bool { d, _ := svc.DefaultOutput(); return near(d.Volume.AveragePercentage(), 75) })
	if got, _ := c.AdjustOutputVolume(ctx, -500); got != 0 {
		t.Errorf("AdjustOutputVolume(-500) = %v, want 0", got)
	}

	got, err = c.SetInputVolume(ctx, 40)
	if err != nil || got != 40 {
		t.Fatalf("SetInputVolume = %v, %v", got, err)
	}
	waitFor(t, "input 40%", func() bool { d, _ := svc.DefaultInput(); return near(d.Volume.AveragePercentage(), 40) })
	if got, _ := c.AdjustInputVolume(ctx, 5); !near(got, 45) {
		t.Errorf("AdjustInputVolume(5) = %v", got)
	}
	vol, err := c.InputVolume(ctx)
	waitFor(t, "input 45%", func() bool { vol, err = c.InputVolume(ctx); return near(vol, 45) })
	if err != nil {
		t.Fatal(err)
	}
}

func TestDaemonMuteMethods(t *testing.T) {
	c, svc, _ := daemonFixture(t)
	ctx := context.Background()
	muted, err := c.ToggleOutputMute(ctx)
	if err != nil || !muted {
		t.Fatalf("ToggleOutputMute = %v, %v", muted, err)
	}
	waitFor(t, "muted", func() bool { d, _ := svc.DefaultOutput(); return d.Muted })
	if err := c.SetOutputMute(ctx, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "unmuted", func() bool { d, _ := svc.DefaultOutput(); return !d.Muted })
	if muted, _ := c.ToggleInputMute(ctx); !muted {
		t.Error("ToggleInputMute did not report muted")
	}
	waitFor(t, "input muted", func() bool { m, _ := c.InputMuted(ctx); return m })
	if err := c.SetInputMute(ctx, false); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonDevicesAndInfo(t *testing.T) {
	c, svc, _ := daemonFixture(t)
	ctx := context.Background()
	sinks, err := c.ListSinks(ctx)
	if err != nil || len(sinks) != 2 || sinks[0] != (DeviceEntry{Index: 1, Name: "speakers", Description: "Speakers"}) {
		t.Fatalf("ListSinks = %v, %v", sinks, err)
	}
	sources, err := c.ListSources(ctx)
	if err != nil || len(sources) != 3 || sources[2].Name != "mic" {
		t.Fatalf("ListSources = %v, %v", sources, err)
	}
	info, err := c.GetDefaultSinkInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"index": "1", "name": "speakers", "description": "Speakers", "volume": "50",
		"muted": "false", "state": "Running", "active_port": "speaker",
	}
	for k, v := range want {
		if info[k] != v {
			t.Errorf("info[%s] = %q, want %q", k, info[k], v)
		}
	}
	src, err := c.GetDefaultSourceInfo(ctx)
	if err != nil || src["name"] != "mic" || src["state"] != "Suspended" {
		t.Errorf("GetDefaultSourceInfo = %v, %v", src, err)
	}
	if _, has := src["active_port"]; has {
		t.Error("a device without an active port reported one")
	}

	if err := c.SetDefaultSink(ctx, 3); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the default sink", func() bool { name, _ := c.DefaultSink(ctx); return name == "headphones" })
	if err := c.SetDefaultSource(ctx, 2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the default source", func() bool { d, ok := svc.DefaultInput(); return ok && d.Name == "speakers.monitor" })
	if msg := dbusFailure(t, c.SetDefaultSink(ctx, 99)); msg != "device 99 (Output) not found" {
		t.Errorf("SetDefaultSink(99) message = %q", msg)
	}
	if msg := dbusFailure(t, c.SetDefaultSource(ctx, 1)); msg != "device 1 (Input) not found" {
		t.Errorf("SetDefaultSource(1) message = %q (1 is a sink)", msg)
	}
}

func TestDaemonProperties(t *testing.T) {
	c, _, bus := daemonFixture(t)
	ctx := context.Background()
	if v, err := c.OutputVolume(ctx); err != nil || v != 50 {
		t.Errorf("OutputVolume = %v, %v", v, err)
	}
	if m, err := c.OutputMuted(ctx); err != nil || m {
		t.Errorf("OutputMuted = %v, %v", m, err)
	}
	if n, err := c.SinkCount(ctx); err != nil || n != 2 {
		t.Errorf("SinkCount = %v, %v", n, err)
	}
	if n, err := c.SourceCount(ctx); err != nil || n != 3 {
		t.Errorf("SourceCount = %v, %v", n, err)
	}
	if name, err := c.DefaultSource(ctx); err != nil || name != "mic" {
		t.Errorf("DefaultSource = %q, %v", name, err)
	}

	obj := bus.Conn(t).Object(ServiceName, ServicePath)
	var all map[string]dbus.Variant
	if err := obj.Call(propertiesInterface+".GetAll", 0, Interface).Store(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != len(propertyNames) || all["DefaultSink"].Value() != "speakers" {
		t.Errorf("GetAll = %v", all)
	}
	err := obj.Call(propertiesInterface+".Set", 0, Interface, "OutputVolume", dbus.MakeVariant(10.0)).Err
	var derr dbus.Error
	if !errors.As(err, &derr) || derr.Name != errPropertyReadOnly {
		t.Errorf("Set = %v, want PropertyReadOnly", err)
	}
	if err := obj.Call(propertiesInterface+".Get", 0, Interface, "Nope").Err; err == nil {
		t.Error("an unknown property read")
	}
	var xml string
	if err := obj.Call("org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&xml); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="com.wayle.Audio1"`, `name="ListSinks"`, `type="a(uss)"`, `name="SinkCount"`} {
		if !strings.Contains(xml, want) {
			t.Errorf("introspection lacks %s", want)
		}
	}
}

func TestDaemonWithoutDefaults(t *testing.T) {
	srv := fixture(t)
	srv.SetDefaults("", "")
	svc := connect(t, srv)
	bus := dbustest.Start(t)
	release, err := NewDaemon(svc).Export(bus.Conn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	c := NewClient(bus.Conn(t))
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"SetOutputVolume":    func() error { _, err := c.SetOutputVolume(ctx, 1); return err },
		"AdjustOutputVolume": func() error { _, err := c.AdjustOutputVolume(ctx, 1); return err },
		"SetOutputMute":      func() error { return c.SetOutputMute(ctx, true) },
		"ToggleOutputMute":   func() error { _, err := c.ToggleOutputMute(ctx); return err },
		"GetDefaultSinkInfo": func() error { _, err := c.GetDefaultSinkInfo(ctx); return err },
	} {
		if msg := dbusFailure(t, call()); msg != "No default output device" {
			t.Errorf("%s message = %q", name, msg)
		}
	}
	for name, call := range map[string]func() error{
		"SetInputVolume":       func() error { _, err := c.SetInputVolume(ctx, 1); return err },
		"ToggleInputMute":      func() error { _, err := c.ToggleInputMute(ctx); return err },
		"GetDefaultSourceInfo": func() error { _, err := c.GetDefaultSourceInfo(ctx); return err },
	} {
		if msg := dbusFailure(t, call()); msg != "No default input device" {
			t.Errorf("%s message = %q", name, msg)
		}
	}
	// The properties fall back to zero values, never errors.
	if v, err := c.OutputVolume(ctx); err != nil || v != 0 {
		t.Errorf("OutputVolume = %v, %v", v, err)
	}
	if name, err := c.DefaultSink(ctx); err != nil || name != "" {
		t.Errorf("DefaultSink = %q, %v", name, err)
	}
}

func TestDaemonSurfacesServerErrors(t *testing.T) {
	srv := fixture(t)
	svc := connect(t, srv)
	bus := dbustest.Start(t)
	release, err := NewDaemon(svc).Export(bus.Conn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	srv.FailNext(native.CmdSetSinkMute, native.ErrAccess)
	msg := dbusFailure(t, NewClient(bus.Conn(t)).SetOutputMute(context.Background(), true))
	if !strings.Contains(msg, "Access denied") {
		t.Errorf("message = %q", msg)
	}
}

func TestDaemonNameIsExclusive(t *testing.T) {
	_, svc, bus := daemonFixture(t)
	if _, err := NewDaemon(svc).Export(bus.Conn(t)); err == nil {
		t.Fatal("a second daemon took the name")
	}
}
