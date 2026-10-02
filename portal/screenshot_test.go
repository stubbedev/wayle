package portal

import (
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/shell/screenshot"
)

// fakeShooter is the shell's com.wayle.Screenshot1.
type fakeShooter struct {
	path  string
	err   dbustest.Var[error]
	modes dbustest.Var[[]string]
}

func (f *fakeShooter) Capture(mode, target string) (string, *dbus.Error) {
	f.modes.Update(func(m []string) []string { return append(m, mode+"|"+target) })
	if err := f.err.Load(); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return f.path, nil
}

func (f *fakeShooter) PickColor() (float64, float64, float64, *dbus.Error) {
	if err := f.err.Load(); err != nil {
		return 0, 0, 0, dbus.MakeFailedError(err)
	}
	return 0.25, 0.5, 1, nil
}

func serveShooter(t *testing.T, r *rig, f *fakeShooter) {
	t.Helper()
	conn := r.bus.Conn(t)
	if err := conn.Export(f, screenshot.ServicePath, screenshot.ServiceName); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(screenshot.ServiceName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
}

const (
	screenshotCall = "org.freedesktop.impl.portal.Screenshot.Screenshot"
	pickColorCall  = "org.freedesktop.impl.portal.Screenshot.PickColor"
)

func TestScreenshotRepliesTheFileURI(t *testing.T) {
	r := newRig(t, nil)
	f := &fakeShooter{path: "/home/u/Pictures/Shot 1#.png"}
	serveShooter(t, r, f)
	code, res := r.interactive(t, screenshotCall, handle, "org.app", "", Vardict{"interactive": dbus.MakeVariant(true)})
	if code != ResponseSuccess || res["uri"].Value() != "file:///home/u/Pictures/Shot%201%23.png" {
		t.Errorf("interactive = %d %v", code, res)
	}
	r.interactive(t, screenshotCall, handle, "org.app", "", Vardict{})
	if got := f.modes.Load(); len(got) != 2 || got[0] != "region|" || got[1] != "screen|" {
		t.Errorf("modes = %v: interactive selects a region, else the whole screen", got)
	}
	code, res = r.interactive(t, pickColorCall, handle, "org.app", "", Vardict{})
	if c, ok := res["color"].Value().([]any); code != ResponseSuccess || !ok || c[0] != 0.25 || c[1] != 0.5 || c[2] != 1.0 {
		t.Errorf("PickColor = %d %v", code, res)
	}
}

func TestScreenshotCancelAndFailure(t *testing.T) {
	r := newRig(t, nil)
	if code, _ := r.interactive(t, screenshotCall, handle, "org.app", "", Vardict{}); code != ResponseOther {
		t.Errorf("no shell = %d", code)
	}
	f := &fakeShooter{}
	serveShooter(t, r, f)
	if code, res := r.interactive(t, screenshotCall, handle, "org.app", "", Vardict{}); code != ResponseCancelled || len(res) != 0 {
		t.Errorf("empty path = %d %v, want cancelled", code, res)
	}
	f.err.Store(errors.New("grim failed"))
	if code, _ := r.interactive(t, screenshotCall, handle, "org.app", "", Vardict{}); code != ResponseOther {
		t.Errorf("capture error = %d", code)
	}
	// A cancelled pick errors in the shell, and replies cancelled.
	if code, res := r.interactive(t, pickColorCall, handle, "org.app", "", Vardict{}); code != ResponseCancelled || len(res) != 0 {
		t.Errorf("pick error = %d %v", code, res)
	}
}
