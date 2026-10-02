package portal

import (
	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/capture"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// InputCaptureIface is present and introspectable but captures nothing
// (inputcapture.rs): a real capture needs the compositor to redirect
// local input to the portal, which wlroots compositors and niri do not
// expose (only KWin and Mutter do). It reports zero capabilities, so
// clients detect it and fall back, answers GetZones truthfully from the
// output layout, and fails every barrier.
const InputCaptureIface = "org.freedesktop.impl.portal.InputCapture"

// zone is one (uuii): width, height, x and y offset.
type zone struct {
	Width, Height uint32
	X, Y          int32
}

type inputCapture struct {
	sessions *sessions
	// zones reads the output layout (outputZones; a fake in tests).
	zones func() []zone
}

func (c inputCapture) iface() dbusx.Interface {
	return dbusx.Interface{
		Name:    InputCaptureIface,
		Methods: inputCaptureObject{c},
		Properties: dbusx.Getters{
			"version":               func() any { return uint32(1) },
			"SupportedCapabilities": func() any { return uint32(0) },
		},
	}
}

// noCapabilities is the session results: nothing can be captured.
func noCapabilities() Vardict { return Vardict{"capabilities": dbus.MakeVariant(uint32(0))} }

// outputZones is every output with a mode, at its position.
func outputZones() []zone {
	c, err := capture.Connect()
	if err != nil {
		return nil
	}
	defer func() { _ = c.Close() }()
	var zones []zone
	for _, o := range c.Outputs() {
		if o.Width > 0 && o.Height > 0 {
			zones = append(zones, zone{uint32(o.Width), uint32(o.Height), o.X, o.Y})
		}
	}
	return zones
}

// inputCaptureObject carries the interface's D-Bus methods.
type inputCaptureObject struct{ c inputCapture }

// CreateSession opens a session (v1).
func (o inputCaptureObject) CreateSession(_, session dbus.ObjectPath, _, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	if err := o.c.sessions.mount(session, func() {}); err != nil {
		warnf("inputcapture: cannot mount session: %v", err)
		return ResponseOther, Vardict{}, nil
	}
	return ResponseSuccess, noCapabilities(), nil
}

// CreateSession2 opens a session (v2: no request, results only).
func (o inputCaptureObject) CreateSession2(session dbus.ObjectPath, _ string, _ Vardict) (Vardict, *dbus.Error) {
	if err := o.c.sessions.mount(session, func() {}); err != nil {
		warnf("inputcapture: cannot mount session: %v", err)
	}
	return noCapabilities(), nil
}

// Start succeeds; nothing is captured.
func (inputCaptureObject) Start(_, _ dbus.ObjectPath, _, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	return ResponseSuccess, Vardict{}, nil
}

// GetZones reports the output layout as zone set 1.
func (o inputCaptureObject) GetZones(_, _ dbus.ObjectPath, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	zones := o.c.zones()
	if zones == nil {
		zones = []zone{}
	}
	return ResponseSuccess, Vardict{"zones": dbus.MakeVariant(zones), "zone_set": dbus.MakeVariant(uint32(1))}, nil
}

// SetPointerBarriers fails every barrier: there is no grab to honour
// them.
func (inputCaptureObject) SetPointerBarriers(_, _ dbus.ObjectPath, _ string, _ Vardict, barriers []Vardict, _ uint32) (uint32, Vardict, *dbus.Error) {
	failed := []uint32{}
	for _, b := range barriers {
		if id, ok := b["barrier_id"].Value().(uint32); ok {
			failed = append(failed, id)
		}
	}
	return ResponseSuccess, Vardict{"failed_barriers": dbus.MakeVariant(failed)}, nil
}

// Enable succeeds; there is nothing to enable.
func (inputCaptureObject) Enable(_ dbus.ObjectPath, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	return ResponseSuccess, Vardict{}, nil
}

// Disable succeeds.
func (inputCaptureObject) Disable(_ dbus.ObjectPath, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	return ResponseSuccess, Vardict{}, nil
}

// Release succeeds.
func (inputCaptureObject) Release(_ dbus.ObjectPath, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	return ResponseSuccess, Vardict{}, nil
}
