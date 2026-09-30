package brightness

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"

	"golang.org/x/sys/unix"
)

// Hotplug over kernel uevents, the Go counterpart of backend/hotplug.rs's
// two udev monitors (backlight add/remove, DRM connector hotplug). The
// Rust uses libudev's monitor; pure Go reads the kernel's own uevent
// netlink group instead, which carries the same ACTION/SUBSYSTEM and
// the DRM HOTPLUG=1 flag without depending on udevd having processed
// the event.

// uevent is one kernel uevent.
type uevent struct {
	Action    string
	Subsystem string
	// Sysname is the device's kernel name, the last DEVPATH element.
	Sysname string
	Props   map[string]string
}

// parseUevent decodes one kernel netlink message:
// "action@devpath\0KEY=VALUE\0...". libudev-format messages (the
// "libudev" magic header, sent to the udev group) are rejected.
func parseUevent(msg []byte) (uevent, error) {
	fields := bytes.Split(msg, []byte{0})
	if len(fields) < 2 || !bytes.Contains(fields[0], []byte("@")) {
		return uevent{}, errors.New("not a kernel uevent")
	}
	ev := uevent{Props: map[string]string{}}
	for _, field := range fields[1:] {
		key, value, ok := bytes.Cut(field, []byte("="))
		if !ok {
			continue
		}
		ev.Props[string(key)] = string(value)
	}
	ev.Action = ev.Props["ACTION"]
	ev.Subsystem = ev.Props["SUBSYSTEM"]
	if devpath := ev.Props["DEVPATH"]; devpath != "" {
		ev.Sysname = path.Base(devpath)
	}
	if ev.Action == "" || ev.Subsystem == "" {
		return uevent{}, errors.New("uevent without ACTION or SUBSYSTEM")
	}
	return ev, nil
}

// isDRMHotplug is is_drm_hotplug: a connector hotplug is a DRM
// "change" carrying HOTPLUG=1; other changes (mode sets, DPMS, EDID
// property updates) omit it and must not trigger a DDC re-scan.
func (ev uevent) isDRMHotplug() bool {
	return ev.Subsystem == "drm" && ev.Action == "change" && ev.Props["HOTPLUG"] == "1"
}

// isBacklightHotplug is a backlight device added or removed.
func (ev uevent) isBacklightHotplug() bool {
	return ev.Subsystem == "backlight" && (ev.Action == "add" || ev.Action == "remove")
}

// ueventReader yields uevents until closed.
type ueventReader interface {
	// Next blocks for the next parsed uevent; it returns an error once
	// Close was called or the socket failed.
	Next() (uevent, error)
	// Close releases the socket and unblocks a pending Next.
	Close() error
}

// kernelGroup is the kernel's uevent multicast group (udevd re-sends
// processed events on group 2 in libudev's own framing).
const kernelGroup = 1

// ueventSocket is a bound NETLINK_KOBJECT_UEVENT socket on the kernel
// group, which unprivileged processes may read. It is non-blocking and
// wrapped in an os.File so reads park on the runtime poller and Close
// interrupts them.
type ueventSocket struct {
	file *os.File
	buf  []byte
}

// listenUevents opens the socket; a var so tests can inject events.
var listenUevents = func() (ueventReader, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, fmt.Errorf("brightness: uevent socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: kernelGroup}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("brightness: uevent bind: %w", err)
	}
	return &ueventSocket{file: os.NewFile(uintptr(fd), "uevent"), buf: make([]byte, 16*1024)}, nil
}

// Next reads datagrams until one parses. A burst larger than the
// socket buffer (ENOBUFS) loses events, not the reader.
func (s *ueventSocket) Next() (uevent, error) {
	for {
		n, err := s.file.Read(s.buf)
		if err != nil {
			if errors.Is(err, unix.ENOBUFS) {
				continue
			}
			return uevent{}, err
		}
		if ev, err := parseUevent(s.buf[:n]); err == nil {
			return ev, nil
		}
	}
}

// Close releases the socket, failing a blocked Next.
func (s *ueventSocket) Close() error { return s.file.Close() }
