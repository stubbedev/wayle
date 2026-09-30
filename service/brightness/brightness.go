// Package brightness reads and sets backlight levels, the Go
// counterpart of crates/wayle-brightness: sysfs enumeration and reads,
// logind SetBrightness as the write path with a direct sysfs fallback,
// and external monitors over DDC/CI (ddc.go) with kernel-uevent
// hotplug (uevent.go).
package brightness

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// BacklightDir is the sysfs class directory for backlight devices; a
// var so tests can repoint it at a fake tree.
var BacklightDir = "/sys/class/backlight"

// Type distinguishes firmware, platform, and raw backlights
// (types/backlight.rs's BacklightType).
type Type uint8

// Backlight types.
const (
	TypeRaw Type = iota
	TypeFirmware
	TypePlatform
	// TypeDDC is an external monitor driven over DDC/CI.
	TypeDDC
)

// TypeFromSysfs parses the sysfs `type` file; unknown values are Raw,
// matching BacklightType::from_sysfs's None fallback.
func TypeFromSysfs(value string) Type {
	switch strings.TrimSpace(value) {
	case "firmware":
		return TypeFirmware
	case "platform":
		return TypePlatform
	}
	return TypeRaw
}

// Device is one backlight snapshot: brightness against max in raw
// units, plus the derived percentage.
type Device struct {
	Name       string
	Type       Type
	Brightness uint32
	Max        uint32
}

// Percentage maps the raw level onto 0..100.
func (d Device) Percentage() float64 {
	if d.Max == 0 {
		return 0
	}
	return float64(d.Brightness) / float64(d.Max) * 100
}

// readAttr reads one trimmed sysfs attribute.
func readAttr(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // the path is BacklightDir plus a validated directory entry
	if err != nil {
		return "", err
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func readUint(path string) (uint32, error) {
	line, err := readAttr(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(line, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("brightness: %s: %w", path, err)
	}
	return uint32(value), nil
}

// ReadDevice reads one device's snapshot from sysfs.
func ReadDevice(name string) (Device, error) {
	base := filepath.Join(BacklightDir, name)
	rawType, err := readAttr(filepath.Join(base, "type"))
	if err != nil {
		return Device{}, err
	}
	brightness, err := readUint(filepath.Join(base, "brightness"))
	if err != nil {
		return Device{}, err
	}
	max, err := readUint(filepath.Join(base, "max_brightness"))
	if err != nil {
		return Device{}, err
	}
	return Device{
		Name:       name,
		Type:       TypeFromSysfs(rawType),
		Brightness: brightness,
		Max:        max,
	}, nil
}

// Enumerate lists every backlight device; an unreadable class
// directory yields an empty list, the Rust shell's warn-and-continue.
func Enumerate() []Device {
	entries, err := os.ReadDir(BacklightDir)
	if err != nil {
		return nil
	}
	var devices []Device
	for _, entry := range entries {
		if device, err := ReadDevice(entry.Name()); err == nil {
			devices = append(devices, device)
		}
	}
	return devices
}

// writeSysfs writes the raw level straight to sysfs; needs video
// group membership, which is why logind is the primary path.
func writeSysfs(name string, value uint32) error {
	return os.WriteFile(filepath.Join(BacklightDir, name, "brightness"), //nolint:gosec // sysfs ignores the mode; the kernel file has its own permissions
		[]byte(strconv.FormatUint(uint64(value), 10)), 0o644)
}

// setLogind drives logind's SetBrightness for the session
// (backend/logind.rs's primary write path).
func setLogind(conn *dbus.Conn, name string, value uint32) error {
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1/session/self")
	return obj.Call("org.freedesktop.login1.Session.SetBrightness", 0, "backlight", name, value).Err
}

// sessionBus connects lazily; a missing session bus is not fatal —
// the write path falls back to sysfs.
var sessionBus = func() (*dbus.Conn, error) {
	return dbus.ConnectSystemBus()
}

// Source is the module's seam: enumerate, watch, and set.
type Source interface {
	// Devices returns the current snapshots.
	Devices(ctx context.Context) ([]Device, error)
	// Subscribe ticks whenever any device's level may have changed.
	// The channel closes when ctx completes; stop releases the watch.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
	// Set applies a percentage to one device, clamped to 0..100.
	Set(ctx context.Context, name string, percent float64) error
}
