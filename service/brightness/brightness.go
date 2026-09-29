// Package brightness reads and sets backlight levels, the Go
// counterpart of crates/wayle-brightness: sysfs enumeration and reads,
// logind SetBrightness as the write path with a direct sysfs fallback.
// External DDC/CI monitors are not ported yet.
package brightness

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
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

// Sysfs is the real source: sysfs reads, logind writes with sysfs
// fallback, inotify watching.
type Sysfs struct {
	mu     sync.Mutex
	bus    *dbus.Conn
	busErr error
}

// NewSysfs connects to the system bus for logind; failure is deferred
// to writes, which fall back to sysfs.
func NewSysfs() *Sysfs {
	conn, err := sessionBus()
	return &Sysfs{bus: conn, busErr: err}
}

// Devices reads every device.
func (s *Sysfs) Devices(context.Context) ([]Device, error) {
	return Enumerate(), nil
}

// Set writes the clamped raw level: logind first, sysfs on failure.
func (s *Sysfs) Set(_ context.Context, name string, percent float64) error {
	device, err := ReadDevice(name)
	if err != nil {
		return err
	}
	clamped := percent
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}
	value := uint32(clamped / 100 * float64(device.Max))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bus != nil {
		if err := setLogind(s.bus, name, value); err == nil {
			return nil
		}
	}
	return writeSysfs(name, value)
}

// Subscribe inotify-watches every device directory for level writes
// and re-checks each second as a cheap drift correction (other
// processes, DDC interactions).
func (s *Sysfs) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return nil, nil, fmt.Errorf("brightness: inotify: %w", err)
	}
	watches := map[string]int{}
	entries, err := os.ReadDir(BacklightDir)
	if err != nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("brightness: %w", err)
	}
	for _, entry := range entries {
		wd, err := unix.InotifyAddWatch(fd, filepath.Join(BacklightDir, entry.Name()), unix.IN_MODIFY)
		if err == nil {
			watches[entry.Name()] = wd
		}
	}

	ticks := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(ticks)
		buf := make([]byte, 4096)
		poll := time.NewTicker(time.Second)
		defer poll.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-poll.C:
				tick(ticks)
			default:
				// Drain inotify without blocking: a read with no
				// pending events would block, so poll readiness via
				// a short SetNonblock cycle.
				_ = unix.SetNonblock(fd, true)
				n, err := unix.Read(fd, buf)
				_ = unix.SetNonblock(fd, false)
				if err == nil && n > 0 {
					tick(ticks)
				} else {
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
	}()
	stop := func() {
		close(done)
		_ = unix.Close(fd)
	}
	return ticks, stop, nil
}

func tick(ticks chan struct{}) {
	select {
	case ticks <- struct{}{}:
	default:
	}
}

// ErrNoDevices is returned when the class directory yields nothing.
var ErrNoDevices = errors.New("brightness: no backlight devices found")
