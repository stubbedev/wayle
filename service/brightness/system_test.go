package brightness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeUevents feeds injected uevents to the System.
type fakeUevents struct {
	events chan uevent
	closed chan struct{}
}

func (f *fakeUevents) Next() (uevent, error) {
	select {
	case ev := <-f.events:
		return ev, nil
	case <-f.closed:
		return uevent{}, errors.New("closed")
	}
}

func (f *fakeUevents) Close() error {
	close(f.closed)
	return nil
}

// newTestSystem builds a System on the fake trees with injected
// uevents; the caller has already faked BacklightDir (and DevDir for
// external monitors).
func newTestSystem(t *testing.T, external bool) *System {
	t.Helper()
	sys, _ := newTestSystemWithEvents(t, external)
	return sys
}

func newTestSystemWithEvents(t *testing.T, external bool) (*System, chan<- uevent) {
	t.Helper()
	events := &fakeUevents{events: make(chan uevent), closed: make(chan struct{})}
	orig, origDrift := listenUevents, driftInterval
	listenUevents = func() (ueventReader, error) { return events, nil }
	// Quiet the drift ticks so every tick a test sees has a cause.
	driftInterval = time.Hour
	t.Cleanup(func() { listenUevents, driftInterval = orig, origDrift })
	sys := NewSystem(external)
	sys.settle = 0
	t.Cleanup(func() { _ = sys.Close() })
	return sys, events.events
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ddcNames(t *testing.T, sys *System) []string {
	t.Helper()
	devices, err := sys.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range devices {
		if d.Type == TypeDDC {
			names = append(names, d.Name)
		}
	}
	return names
}

func TestParseUevent(t *testing.T) {
	msg := []byte("change@/devices/pci0000:00/0000:00:02.0/drm/card1\x00ACTION=change\x00DEVPATH=/devices/pci0000:00/0000:00:02.0/drm/card1\x00SUBSYSTEM=drm\x00HOTPLUG=1\x00SEQNUM=4242\x00")
	ev, err := parseUevent(msg)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Action != "change" || ev.Subsystem != "drm" || ev.Sysname != "card1" || !ev.isDRMHotplug() {
		t.Errorf("event = %+v", ev)
	}

	// A mode set or DPMS change carries no HOTPLUG flag.
	modeset, _ := parseUevent([]byte("change@/x/card1\x00ACTION=change\x00DEVPATH=/x/card1\x00SUBSYSTEM=drm\x00"))
	if modeset.isDRMHotplug() {
		t.Error("a change without HOTPLUG=1 counted as a hotplug")
	}

	added, _ := parseUevent([]byte("add@/devices/x/backlight/intel_backlight\x00ACTION=add\x00DEVPATH=/devices/x/backlight/intel_backlight\x00SUBSYSTEM=backlight\x00"))
	if !added.isBacklightHotplug() || added.Sysname != "intel_backlight" {
		t.Errorf("backlight add = %+v", added)
	}
	changed, _ := parseUevent([]byte("change@/devices/x/backlight/b\x00ACTION=change\x00DEVPATH=/devices/x/backlight/b\x00SUBSYSTEM=backlight\x00"))
	if changed.isBacklightHotplug() {
		t.Error("a backlight change counted as add/remove")
	}

	for name, bad := range map[string][]byte{
		"libudev framing": []byte("libudev\x00\xfe\xed\xca\xfe"),
		"no action":       []byte("add@/x\x00SUBSYSTEM=drm\x00"),
		"empty":           nil,
	} {
		if _, err := parseUevent(bad); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

func TestExternalMonitorsAppearAfterTheDetachedScan(t *testing.T) {
	fakeBacklightDir(t, nil)
	fake := newFakeI2C(t)
	fake.plug(t, 6, &fakeMonitor{current: 40, max: 100})
	sys := newTestSystem(t, true)
	waitFor(t, "the initial DDC scan", func() bool { return len(ddcNames(t, sys)) == 1 })
}

func TestExternalDisabledNeverScans(t *testing.T) {
	fakeBacklightDir(t, nil)
	fake := newFakeI2C(t)
	fake.plug(t, 6, &fakeMonitor{current: 40, max: 100})
	sys, events := newTestSystemWithEvents(t, false)
	events <- uevent{Action: "change", Subsystem: "drm", Props: map[string]string{"HOTPLUG": "1"}}
	time.Sleep(50 * time.Millisecond)
	if names := ddcNames(t, sys); len(names) != 0 {
		t.Errorf("enable-external=false still found %v", names)
	}
}

func TestDRMHotplugRescansAndTicks(t *testing.T) {
	fakeBacklightDir(t, nil)
	fake := newFakeI2C(t)
	fake.plug(t, 1, &fakeMonitor{current: 20, max: 100})
	sys, events := newTestSystemWithEvents(t, true)
	ticks, stop, err := sys.Subscribe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	waitFor(t, "the initial DDC scan", func() bool { return len(ddcNames(t, sys)) == 1 })
	<-ticks

	fake.plug(t, 8, &fakeMonitor{current: 70, max: 100})
	// A non-hotplug DRM change must not rescan.
	events <- uevent{Action: "change", Subsystem: "drm", Props: map[string]string{}}
	time.Sleep(50 * time.Millisecond)
	if names := ddcNames(t, sys); len(names) != 1 {
		t.Fatalf("a mode-set change rescanned: %v", names)
	}

	events <- uevent{Action: "change", Subsystem: "drm", Props: map[string]string{"HOTPLUG": "1"}}
	waitFor(t, "the hotplug rescan", func() bool { return len(ddcNames(t, sys)) == 2 })
	select {
	case <-ticks:
	case <-time.After(3 * time.Second):
		t.Fatal("no tick after a monitor appeared")
	}

	fake.unplug(t, 8)
	events <- uevent{Action: "change", Subsystem: "drm", Props: map[string]string{"HOTPLUG": "1"}}
	waitFor(t, "the unplug rescan", func() bool { return len(ddcNames(t, sys)) == 1 })
}

func TestSetRoutesDDCNamesToTheMonitor(t *testing.T) {
	fakeBacklightDir(t, map[string]struct {
		typ      string
		cur, max uint32
	}{"intel_backlight": {"raw", 3000, 12000}})
	fake := newFakeI2C(t)
	monitor := &fakeMonitor{current: 10, max: 200}
	fake.plug(t, 2, monitor)
	sys := newTestSystem(t, true)
	waitFor(t, "the initial DDC scan", func() bool { return len(ddcNames(t, sys)) == 1 })

	if err := sys.Set(context.Background(), "ddci2c-2", 50); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the DDC write", func() bool {
		w := monitor.written()
		return len(w) > 0 && w[len(w)-1] == 100
	})
	devices, _ := sys.Devices(context.Background())
	for _, d := range devices {
		if d.Name == "ddci2c-2" && d.Brightness != 100 {
			t.Errorf("cached DDC level = %d, want 100", d.Brightness)
		}
	}
	// The internal panel still goes through sysfs, untouched by DDC.
	if device, _ := ReadDevice("intel_backlight"); device.Brightness != 3000 {
		t.Errorf("a DDC write moved the panel to %d", device.Brightness)
	}
	// An unknown name is neither: it errors instead of being dropped.
	if err := sys.Set(context.Background(), "ddci2c-9", 50); err == nil {
		t.Error("Set on an unknown device: want an error")
	}
}

func TestBacklightHotplugWatchesTheNewDevice(t *testing.T) {
	fakeBacklightDir(t, nil)
	sys, events := newTestSystemWithEvents(t, false)
	ticks, stop, err := sys.Subscribe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	base := filepath.Join(BacklightDir, "acpi_video0")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	for attr, value := range map[string]string{"type": "firmware\n", "brightness": "5", "max_brightness": "10"} {
		if err := os.WriteFile(filepath.Join(base, attr), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	events <- uevent{Action: "add", Subsystem: "backlight", Sysname: "acpi_video0", Props: map[string]string{}}
	waitFor(t, "the hotplug tick", func() bool {
		select {
		case <-ticks:
			return true
		default:
			return false
		}
	})
	// Drain the drift ticks, then a level write must tick via the new
	// watch.
	time.Sleep(10 * time.Millisecond)
	if err := writeSysfs("acpi_video0", 7); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ticks:
	case <-time.After(3 * time.Second):
		t.Fatal("no tick for a write to the hotplugged device")
	}
}

func TestSubscribeStopsAndRefusesAfterClose(t *testing.T) {
	fakeBacklightDir(t, nil)
	sys := newTestSystem(t, false)
	ticks, stop, err := sys.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	for range ticks { // drain until the forwarder closes
	}
	_ = sys.Close()
	if _, _, err := sys.Subscribe(context.Background()); err == nil {
		t.Error("Subscribe after Close: want an error")
	}
}

func TestUeventSocketCloseUnblocksNext(t *testing.T) {
	reader, err := listenUevents()
	if err != nil {
		t.Skipf("no uevent netlink here: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := reader.Next(); err != nil {
				done <- err
				return
			}
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("Next after Close returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Next")
	}
}
