package portal

import (
	"os"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// ClipboardIface bridges the Wayland selection to clipboard-enabled
// RemoteDesktop sessions (clipboard/mod.rs): they read the current
// selection, become its owner, and serve its data on demand.
const ClipboardIface = "org.freedesktop.impl.portal.Clipboard"

// clipDevice is the Wayland side: the seat's data-control device
// (waylandClipboard), a fake in tests.
type clipDevice interface {
	// own claims the clipboard offering mimes; each receiver's request
	// is passed its pipe, which the callee closes.
	own(mimes []string, pass func(mime string, pipe *os.File)) error
	// read asks the current selection's owner for mime and returns the
	// read end of the pipe it streams into.
	read(mime string) (*os.File, error)
}

// clipboardBridge is the interface's state.
type clipboardBridge struct {
	conn *dbus.Conn
	// start brings the device up, calling ownerChanged on every
	// selection change; it runs on first RequestClipboard.
	start func(ownerChanged func()) (clipDevice, error)

	mu        sync.Mutex
	dev       clipDevice
	enabled   map[dbus.ObjectPath]bool
	serial    uint32
	transfers map[uint32]*os.File
}

func newClipboardBridge(conn *dbus.Conn, start func(func()) (clipDevice, error)) *clipboardBridge {
	return &clipboardBridge{conn: conn, start: start, enabled: map[dbus.ObjectPath]bool{}, transfers: map[uint32]*os.File{}}
}

func (c *clipboardBridge) iface() dbusx.Interface {
	return dbusx.Interface{
		Name:       ClipboardIface,
		Methods:    clipboardObject{c},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// device is the started device, nil before (or without) one.
func (c *clipboardBridge) device() clipDevice {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dev
}

// ensure starts the device once; it reports whether one is up.
func (c *clipboardBridge) ensure() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev != nil {
		return true
	}
	dev, err := c.start(c.ownerChanged)
	if err != nil {
		warnf("clipboard unavailable on this compositor: %v", err)
		return false
	}
	c.dev = dev
	return true
}

// sessions is every clipboard-enabled session.
func (c *clipboardBridge) sessions() []dbus.ObjectPath {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]dbus.ObjectPath, 0, len(c.enabled))
	for s := range c.enabled {
		out = append(out, s)
	}
	return out
}

func (c *clipboardBridge) ownerChanged() {
	for _, s := range c.sessions() {
		_ = c.conn.Emit(ObjectPath, ClipboardIface+".SelectionOwnerChanged", s, Vardict{})
	}
}

// transfer files a receiver's pipe under a fresh serial (from 1) and
// asks the sessions for the data.
func (c *clipboardBridge) transfer(mime string, pipe *os.File) {
	c.mu.Lock()
	c.serial++
	serial := c.serial
	c.transfers[serial] = pipe
	c.mu.Unlock()
	for _, s := range c.sessions() {
		_ = c.conn.Emit(ObjectPath, ClipboardIface+".SelectionTransfer", s, mime, serial)
	}
}

// dropTransfers closes the pipes no session took: a new selection
// replaces what they were asking for, and an open pipe would keep its
// receiver waiting.
func (c *clipboardBridge) dropTransfers() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for serial, f := range c.transfers {
		_ = f.Close()
		delete(c.transfers, serial)
	}
}

// clipboardObject carries the interface's D-Bus methods.
type clipboardObject struct{ c *clipboardBridge }

// RequestClipboard enables clipboard access for a session.
func (o clipboardObject) RequestClipboard(session dbus.ObjectPath, _ Vardict) *dbus.Error {
	if o.c.ensure() {
		o.c.mu.Lock()
		o.c.enabled[session] = true
		o.c.mu.Unlock()
	}
	return nil
}

// SetSelection makes the session the owner of the given mime types.
func (o clipboardObject) SetSelection(_ dbus.ObjectPath, options Vardict) *dbus.Error {
	dev := o.c.device()
	if dev == nil {
		return nil
	}
	mimes, ok := options["mime_types"].Value().([]string)
	if m, single := optString(options, "mime_type"); !ok && single {
		mimes = []string{m}
	}
	o.c.dropTransfers()
	if err := dev.own(mimes, o.c.transfer); err != nil {
		warnf("clipboard: set selection: %v", err)
	}
	return nil
}

// SelectionWrite hands over the pipe of a pending transfer, for the
// app to fill; godbus closes our copy once the reply is out.
func (o clipboardObject) SelectionWrite(_ dbus.ObjectPath, serial uint32) (*os.File, *dbus.Error) {
	o.c.mu.Lock()
	f, ok := o.c.transfers[serial]
	delete(o.c.transfers, serial)
	o.c.mu.Unlock()
	if !ok {
		return nil, dbusx.Failed("no pending clipboard transfer")
	}
	return f, nil
}

// SelectionWriteDone acknowledges a finished transfer: the pipe was
// already the app's.
func (clipboardObject) SelectionWriteDone(_ dbus.ObjectPath, _ uint32, _ bool) *dbus.Error {
	return nil
}

// SelectionRead streams the current selection under mime.
func (o clipboardObject) SelectionRead(_ dbus.ObjectPath, mime string) (*os.File, *dbus.Error) {
	dev := o.c.device()
	if dev == nil {
		return nil, dbusx.Failed("no clipboard selection")
	}
	f, err := dev.read(mime)
	if err != nil {
		return nil, dbusx.Failed("no clipboard selection")
	}
	return f, nil
}
