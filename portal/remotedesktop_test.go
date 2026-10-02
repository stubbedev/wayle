package portal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeSink records the injected input.
type fakeSink struct {
	events dbustest.Var[[]string]
	closed dbustest.Var[bool]
	keymap []byte
}

func (f *fakeSink) log(s string) { f.events.Update(func(e []string) []string { return append(e, s) }) }

func (f *fakeSink) Motion(dx, dy float64) error { f.log(sprint("motion", dx, dy)); return nil }
func (f *fakeSink) MotionAbsolute(x, y, w, h uint32) error {
	f.log(sprint("abs", x, y, w, h))
	return nil
}
func (f *fakeSink) Button(c uint32, p bool) error { f.log(sprint("button", c, p)); return nil }
func (f *fakeSink) Axis(a uint32, v float64) error {
	f.log(sprint("axis", a, v))
	return nil
}

func (f *fakeSink) AxisDiscrete(a uint32, v float64, s int32) error {
	f.log(sprint("discrete", a, v, s))
	return nil
}
func (f *fakeSink) Key(c uint32, p bool) error { f.log(sprint("key", c, p)); return nil }
func (f *fakeSink) Keymap() []byte             { return f.keymap }
func (f *fakeSink) Close()                     { f.closed.Store(true) }

func sprint(parts ...any) string { return strings.TrimSuffix(fmt.Sprintln(parts...), "\n") }

// testKeymap is an explicit two-key keymap: <AC01> types a / A (evdev
// 30), <LFSH> is shift.
const testKeymap = `xkb_keymap {
xkb_keycodes "test" { minimum = 8; maximum = 255; <AC01> = 38; <LFSH> = 50; };
xkb_types "test" {
  virtual_modifiers NumLock;
  type "ONE_LEVEL" { modifiers = none; level_name[Level1] = "Any"; };
  type "ALPHABETIC" { modifiers = Shift+Lock; map[Shift] = Level2; map[Lock] = Level2; level_name[Level1] = "Base"; level_name[Level2] = "Caps"; };
};
xkb_compat "test" { };
xkb_symbols "test" {
  key <AC01> { type = "ALPHABETIC", [ a, A ] };
  key <LFSH> { [ Shift_L ] };
  modifier_map Shift { <LFSH> };
};
};
`

func remoteRig(t *testing.T, dialErr error) (*rig, *fakeSink) {
	sink := &fakeSink{keymap: []byte(testKeymap)}
	r := newRig(t, func(b *Backend) {
		b.remote = newRemoteDesktop(b.conn, b.sessions, b.sizes, func() (inputSink, error) {
			if dialErr != nil {
				return nil, dialErr
			}
			return sink, nil
		})
	})
	return r, sink
}

func (r *rig) remoteStart(t *testing.T, options Vardict) (uint32, map[string]dbus.Variant) {
	t.Helper()
	r.interactive(t, RemoteDesktopIface+".CreateSession", handle, sessA, "org.app", Vardict{})
	r.interactive(t, RemoteDesktopIface+".SelectDevices", handle, sessA, "org.app", options)
	return r.interactive(t, RemoteDesktopIface+".Start", handle, sessA, "org.app", "", Vardict{})
}

func (r *rig) notify(t *testing.T, method string, args ...any) {
	t.Helper()
	if err := r.obj.Call(RemoteDesktopIface+"."+method, 0, append([]any{sessA, Vardict{}}, args...)...).Err; err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func TestRemoteDesktopAsksFirst(t *testing.T) {
	r, sink := remoteRig(t, nil)
	if code, _ := r.remoteStart(t, Vardict{}); code != ResponseCancelled {
		t.Errorf("no shell = %d", code)
	}
	d := serveDialogs(t, r, false)
	if code, _ := r.remoteStart(t, Vardict{}); code != ResponseCancelled {
		t.Errorf("denied = %d", code)
	}
	if got := d.asked.Load(); len(got) == 0 {
		t.Error("no consent asked")
	}
	r.notify(t, "NotifyPointerMotion", 1.0, 2.0)
	if len(sink.events.Load()) != 0 {
		t.Error("input before an allowed Start")
	}
	d.yes.Store(true)
	code, res := r.remoteStart(t, Vardict{"types": dbus.MakeVariant(devicePointer)})
	if code != ResponseSuccess || res["devices"].Value() != devicePointer {
		t.Errorf("Start = %d %v", code, res)
	}
}

func TestRemoteDesktopReplaysInput(t *testing.T) {
	r, sink := remoteRig(t, nil)
	serveDialogs(t, r, true)
	if code, res := r.remoteStart(t, Vardict{}); code != ResponseSuccess || res["devices"].Value() != uint32(3) {
		t.Fatalf("Start = %d %v", code, res)
	}
	r.backend.sizes.set(40, 1920, 1080)
	r.notify(t, "NotifyPointerMotion", 1.5, -2.0)
	r.notify(t, "NotifyPointerMotionAbsolute", uint32(40), 100.0, -5.0)
	r.notify(t, "NotifyPointerMotionAbsolute", uint32(99), 1.0, 1.0) // unknown stream: dropped
	r.notify(t, "NotifyPointerButton", int32(0x110), uint32(1))
	r.notify(t, "NotifyPointerAxis", 0.0, 3.0)
	r.notify(t, "NotifyPointerAxis", 2.0, 0.0)
	r.notify(t, "NotifyPointerAxisDiscrete", uint32(1), int32(-2))
	r.notify(t, "NotifyPointerAxisDiscrete", uint32(7), int32(1))
	r.notify(t, "NotifyKeyboardKeycode", int32(30), uint32(1))
	r.notify(t, "NotifyKeyboardKeysym", int32(0x41), uint32(1)) // A: shift + a
	r.notify(t, "NotifyKeyboardKeysym", int32(0x41), uint32(0))
	r.notify(t, "NotifyKeyboardKeysym", int32(0x61), uint32(1)) // a
	r.notify(t, "NotifyKeyboardKeysym", int32(0x7a), uint32(1)) // z: not in the keymap
	r.notify(t, "NotifyTouchDown", uint32(40), uint32(0), 1.0, 1.0)
	want := []string{
		"motion 1.5 -2",
		"abs 100 0 1920 1080",
		"button 272 true",
		"axis 0 3",
		"axis 1 2",
		"discrete 1 -30 -2",
		"discrete 0 15 1",
		"key 30 true",
		"key 42 true", "key 30 true",
		"key 30 false", "key 42 false",
		"key 30 true",
	}
	if got := sink.events.Load(); !slices.Equal(got, want) {
		t.Errorf("input =\n%q\nwant\n%q", got, want)
	}
	if err := r.client.Object(BusName, sessA).Call(SessionIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	if !sink.closed.Load() {
		t.Error("the devices outlived the session")
	}
}

func TestRemoteDesktopWithoutVirtualInput(t *testing.T) {
	r, _ := remoteRig(t, errors.New("no zwlr_virtual_pointer_manager_v1"))
	serveDialogs(t, r, true)
	if code, _ := r.remoteStart(t, Vardict{}); code != ResponseOther {
		t.Errorf("Start = %d, want other", code)
	}
	if got := keysymTable([]byte("not a keymap")); len(got) != 0 {
		t.Errorf("a garbage keymap mapped %v", got)
	}
}

func TestRemoteDesktopConnectToEIS(t *testing.T) {
	r, sink := remoteRig(t, nil)
	serveDialogs(t, r, true)
	r.interactive(t, RemoteDesktopIface+".CreateSession", handle, sessA, "org.app", Vardict{})
	var fd dbus.UnixFD
	err := r.obj.Call(RemoteDesktopIface+".ConnectToEIS", 0, sessA, "org.app", Vardict{}).Store(&fd)
	var de dbus.Error
	if !errors.As(err, &de) || de.Body[0] != "no active remote-desktop session" {
		t.Errorf("ConnectToEIS before Start = %v", err)
	}
	r.interactive(t, RemoteDesktopIface+".Start", handle, sessA, "org.app", "", Vardict{})
	if err := r.obj.Call(RemoteDesktopIface+".ConnectToEIS", 0, sessA, "org.app", Vardict{}).Store(&fd); err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(fd), "eis")
	defer f.Close()
	// An EIS server answers on the other end: it opens with its
	// handshake version, ei_handshake (object 0) opcode 0.
	_ = f.SetReadDeadline(time.Now().Add(2 * time.Second))
	head := make([]byte, 20)
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint64(head) != 0 || binary.LittleEndian.Uint32(head[8:]) != 20 || binary.LittleEndian.Uint32(head[12:]) != 0 || binary.LittleEndian.Uint32(head[16:]) != 1 {
		t.Errorf("first message = %x", head)
	}

	// The handler replays onto the session's devices.
	in := r.backend.remote.input(sessA)
	e := eisInput{in}
	e.ScrollDiscrete(-120, 240)
	e.ScrollDiscrete(0, 0)
	e.Scroll(1, 0)
	e.Keysym(0x41, true)
	want := []string{"discrete 0 30 2", "discrete 1 -15 -1", "axis 1 1", "key 42 true", "key 30 true"}
	if got := sink.events.Load(); !slices.Equal(got, want) {
		t.Errorf("replayed %q, want %q", got, want)
	}
}
