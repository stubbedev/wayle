package main

import (
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/portal"
	"github.com/stubbedev/wayle/shell/sharepicker"
)

// fakePicker is the shell's com.wayle.SharePicker1 answering with a
// fixed selection.
type fakePicker struct {
	selection string
	got       *dbustest.Var[[]any]
}

func (f fakePicker) Pick(windowList string, allowToken, multiple bool) (string, *dbus.Error) {
	f.got.Store([]any{windowList, allowToken, multiple})
	return f.selection, nil
}

func servePicker(t *testing.T, selection string) *dbustest.Var[[]any] {
	t.Helper()
	conn := dbustest.SessionConn(t)
	got := &dbustest.Var[[]any]{}
	if err := conn.Export(fakePicker{selection, got}, sharepicker.ServicePath, sharepicker.ServiceName); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(sharepicker.ServiceName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSharePickerPrintsOnlyTheSelection(t *testing.T) {
	dbustest.Session(t)
	t.Setenv("XDPH_WINDOW_SHARING_LIST", "1[HC>]kitty[HT>]~[HE>]")
	got := servePicker(t, "window:1")
	stdout, stderr, code := runCaptured(t, false, "portal", "share-picker", "--allow-token")
	if code != 0 || stdout != "[SELECTION]window:1\n" || stderr != "" {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if args := got.Load(); len(args) != 3 || args[0] != "1[HC>]kitty[HT>]~[HE>]" || args[1] != true || args[2] != false {
		t.Errorf("Pick args = %v: the window list, the token flag, never multiple", args)
	}
}

func TestSharePickerCancelAndNoShell(t *testing.T) {
	dbustest.Session(t)
	stdout, stderr, code := runCaptured(t, false, "portal", "share-picker")
	if code != 1 || stdout != "" || stderr != "SharePicker service not running. Start wayle shell first.\n" {
		t.Errorf("no shell: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	servePicker(t, "")
	stdout, stderr, code = runCaptured(t, false, "portal", "share-picker")
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("cancel: code %d stdout %q stderr %q; a cancel prints nothing", code, stdout, stderr)
	}
}

func TestPortalBackendRefusesAnOwnedName(t *testing.T) {
	dbustest.Session(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := dbustest.SessionConn(t).RequestName(portal.BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"portal"}, {"portal", "run"}} {
		stdout, stderr, code := runCaptured(t, false, args...)
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "portal backend failed: cannot request D-Bus name: ") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, stdout, stderr)
		}
	}
}
