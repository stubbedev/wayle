package brightness

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeBacklightDir builds a sysfs-looking tree in a temp dir and
// repoints the package constants for the test's lifetime.
func fakeBacklightDir(t *testing.T, devices map[string]struct {
	typ      string
	cur, max uint32
},
) {
	t.Helper()
	dir := t.TempDir()
	for name, spec := range devices {
		base := filepath.Join(dir, name)
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		for attr, value := range map[string]string{
			"type":           spec.typ + "\n",
			"brightness":     itoa(spec.cur),
			"max_brightness": itoa(spec.max),
		} {
			if err := os.WriteFile(filepath.Join(base, attr), []byte(value), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	orig := BacklightDir
	BacklightDir = dir
	origBus := sessionBus
	sessionBus = func() (*dbus.Conn, error) { return nil, os.ErrPermission }
	t.Cleanup(func() {
		BacklightDir = orig
		sessionBus = origBus
	})
}

func itoa(v uint32) string {
	return strconv.FormatUint(uint64(v), 10)
}

func TestEnumerateReadsSysfs(t *testing.T) {
	fakeBacklightDir(t, map[string]struct {
		typ      string
		cur, max uint32
	}{
		"intel_backlight": {"raw", 3000, 12000},
		"acpi_video0":     {"firmware", 50, 100},
	})
	devices := Enumerate()
	if len(devices) != 2 {
		t.Fatalf("devices = %+v, want 2", devices)
	}
	byName := map[string]Device{}
	for _, d := range devices {
		byName[d.Name] = d
	}
	intel := byName["intel_backlight"]
	if intel.Type != TypeRaw || intel.Brightness != 3000 || intel.Max != 12000 {
		t.Errorf("intel = %+v", intel)
	}
	if got := intel.Percentage(); got < 24.9 || got > 25.1 {
		t.Errorf("percentage = %v, want 25", got)
	}
	if byName["acpi_video0"].Type != TypeFirmware {
		t.Errorf("acpi type = %v, want firmware", byName["acpi_video0"].Type)
	}
}

func TestTypeFromSysfs(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Type
	}{
		{"raw\n", TypeRaw},
		{"firmware", TypeFirmware},
		{"platform", TypePlatform},
		{"虚拟", TypeRaw}, // unknown values fall back to raw
	} {
		if got := TypeFromSysfs(tc.raw); got != tc.want {
			t.Errorf("TypeFromSysfs(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestSetFallsBackToSysfs(t *testing.T) {
	fakeBacklightDir(t, map[string]struct {
		typ      string
		cur, max uint32
	}{
		"intel_backlight": {"raw", 3000, 12000},
	})
	source := newTestSystem(t, false)
	if err := source.Set(context.Background(), "intel_backlight", 50); err != nil {
		t.Fatalf("Set: %v", err)
	}
	device, err := ReadDevice("intel_backlight")
	if err != nil {
		t.Fatal(err)
	}
	if device.Brightness != 6000 {
		t.Errorf("brightness = %d, want 6000 (50%% of 12000)", device.Brightness)
	}

	// Clamping keeps the write in range even for wild input.
	if err := source.Set(context.Background(), "intel_backlight", 900); err != nil {
		t.Fatal(err)
	}
	if device, _ := ReadDevice("intel_backlight"); device.Brightness != 12000 {
		t.Errorf("clamped brightness = %d, want max", device.Brightness)
	}
}

func TestSubscribeTicksOnWrites(t *testing.T) {
	fakeBacklightDir(t, map[string]struct {
		typ      string
		cur, max uint32
	}{
		"intel_backlight": {"raw", 3000, 12000},
	})
	source := newTestSystem(t, false)
	ctx := t.Context()
	ticks, stop, err := source.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := writeSysfs("intel_backlight", 9000); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ticks:
	case <-time.After(3 * time.Second):
		t.Fatal("no tick within 3s of a brightness write")
	}
}
