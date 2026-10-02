package portal

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// interactive calls a (u, a{sv}) method.
func (r *rig) interactive(t *testing.T, method string, args ...any) (uint32, map[string]dbus.Variant) {
	t.Helper()
	var code uint32
	var results map[string]dbus.Variant
	if err := r.obj.Call(method, 0, args...).Store(&code, &results); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return code, results
}

// spawned records the programs the backend hands off to; fail makes
// the named program fail to start.
type spawned struct {
	argv dbustest.Var[[][]string]
	fail map[string]bool
}

func (s *spawned) spawn(argv []string) error {
	s.argv.Update(func(all [][]string) [][]string { return append(all, argv) })
	if s.fail[argv[0]] {
		return os.ErrNotExist
	}
	return nil
}

func spawnRig(t *testing.T, fail ...string) (*rig, *spawned) {
	s := &spawned{fail: map[string]bool{}}
	for _, f := range fail {
		s.fail[f] = true
	}
	return newRig(t, func(b *Backend) { b.spawn = s.spawn }), s
}

const handle = dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_2/t")

func TestBackgroundAllowsAndAutostarts(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	r := newRig(t, nil)
	const iface = "org.freedesktop.impl.portal.Background"
	code, res := r.interactive(t, iface+".NotifyBackground", handle, "org.app", "App")
	if code != ResponseSuccess || res["result"].Value() != uint32(1) {
		t.Errorf("NotifyBackground = %d %v, want allow", code, res)
	}
	var state map[string]dbus.Variant
	if err := r.obj.Call(iface+".GetAppState", 0).Store(&state); err != nil || len(state) != 0 {
		t.Errorf("GetAppState = %v, %v", state, err)
	}

	var enabled bool
	if err := r.obj.Call(iface+".EnableAutostart", 0, "org.app", true, []string{"my-app", "--gapless"}, uint32(1)).Store(&enabled); err != nil || !enabled {
		t.Fatalf("EnableAutostart on = %v, %v", enabled, err)
	}
	entry := filepath.Join(cfgHome, "autostart", "org.app.desktop")
	got, err := os.ReadFile(entry)
	want := "[Desktop Entry]\nType=Application\nName=org.app\nExec=my-app --gapless\nX-Flatpak=org.app\nDBusActivatable=true\nX-GNOME-Autostart-enabled=true\n"
	if err != nil || string(got) != want {
		t.Errorf("entry = %q, %v", got, err)
	}
	// Without the activatable flag there is no DBusActivatable line.
	_ = r.obj.Call(iface+".EnableAutostart", 0, "org.app", true, []string{"app"}, uint32(0)).Store(&enabled)
	if got, _ := os.ReadFile(entry); strings.Contains(string(got), "DBusActivatable") {
		t.Errorf("a plain app's entry = %q", got)
	}
	if err := r.obj.Call(iface+".EnableAutostart", 0, "org.app", false, []string{}, uint32(0)).Store(&enabled); err != nil || enabled {
		t.Errorf("EnableAutostart off = %v, %v", enabled, err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Error("disabling left the entry")
	}
}

func TestAutostartPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if p, ok := autostartPath("org.app"); !ok || p != "/home/u/.config/autostart/org.app.desktop" {
		t.Errorf("path = %q, %v", p, ok)
	}
	_ = os.Unsetenv("HOME")
	if _, ok := autostartPath("org.app"); ok {
		t.Error("a path without HOME or XDG_CONFIG_HOME")
	}
}

func TestUsbGrantsTheRequestedAccess(t *testing.T) {
	r := newRig(t, nil)
	devices := []requestedDevice{{
		ID:     "dev0",
		Info:   Vardict{"vendor": dbus.MakeVariant("acme")},
		Access: Vardict{"writable": dbus.MakeVariant(true)},
	}}
	code, res := r.interactive(t, "org.freedesktop.impl.portal.Usb.AcquireDevices", handle, "", "org.app", devices, Vardict{})
	var granted []grantedDevice
	if err := dbus.Store([]any{res["devices"].Value()}, &granted); err != nil {
		t.Fatal(err)
	}
	if code != ResponseSuccess || len(granted) != 1 || granted[0].ID != "dev0" || granted[0].Access["writable"].Value() != true || len(granted[0].Access) != 1 {
		t.Errorf("AcquireDevices = %d %v", code, granted)
	}
}

const emailIfaceName = "org.freedesktop.impl.portal.Email.ComposeEmail"

func TestEmailOpensAMailtoURI(t *testing.T) {
	r, s := spawnRig(t)
	opts := Vardict{
		"addresses": dbus.MakeVariant([]string{"x@y.com", "z@w.com"}),
		"address":   dbus.MakeVariant("a@b.com"),
		"cc":        dbus.MakeVariant([]string{"c@d.com"}),
		"subject":   dbus.MakeVariant("Hi there"),
		"body":      dbus.MakeVariant("Line & stuff~"),
	}
	if code, _ := r.interactive(t, emailIfaceName, handle, "org.app", "", opts); code != ResponseSuccess {
		t.Errorf("code = %d", code)
	}
	want := []string{"xdg-open", "mailto:x@y.com,z@w.com,a@b.com?cc=c%40d.com&subject=Hi%20there&body=Line%20%26%20stuff~"}
	if got := s.argv.Load(); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("spawned %q, want %q", got, want)
	}
	if mailto(Vardict{}) != "mailto:" {
		t.Errorf("empty mailto = %q", mailto(Vardict{}))
	}
}

func TestEmailAttachesThroughXdgEmail(t *testing.T) {
	attachment := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(attachment, []byte("attached"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(attachment)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	opts := func() Vardict {
		return Vardict{
			"address":        dbus.MakeVariant("a@b.com"),
			"subject":        dbus.MakeVariant("S"),
			"bcc":            dbus.MakeVariant([]string{"h@i.com"}),
			"attachment_fds": dbus.MakeVariant([]dbus.UnixFD{dbus.UnixFD(f.Fd())}),
		}
	}
	r, s := spawnRig(t)
	r.interactive(t, emailIfaceName, handle, "org.app", "", opts())
	got := s.argv.Load()
	if len(got) != 1 || got[0][0] != "xdg-email" {
		t.Fatalf("spawned %q", got)
	}
	argv := got[0]
	copied := argv[len(argv)-2]
	if want := []string{"xdg-email", "--utf8", "--subject", "S", "--bcc", "h@i.com", "--attach", copied, "a@b.com"}; !slices.Equal(argv, want) {
		t.Errorf("argv = %q", argv)
	}
	t.Cleanup(func() { _ = os.Remove(copied) })
	if b, err := os.ReadFile(copied); err != nil || string(b) != "attached" {
		t.Errorf("copied attachment = %q, %v", b, err)
	}

	// No xdg-email: the mailto fallback still opens, without the files.
	r, s = spawnRig(t, "xdg-email")
	if code, _ := r.interactive(t, emailIfaceName, handle, "org.app", "", opts()); code != ResponseSuccess {
		t.Errorf("fallback code = %d", code)
	}
	if got := s.argv.Load(); len(got) != 2 || got[1][0] != "xdg-open" || got[1][1] != "mailto:a@b.com?bcc=h%40i.com&subject=S" {
		t.Errorf("fallback spawned %q", got)
	}
}

func TestEmailFailsWithoutAHandler(t *testing.T) {
	r, _ := spawnRig(t, "xdg-open")
	if code, _ := r.interactive(t, emailIfaceName, handle, "org.app", "", Vardict{}); code != ResponseOther {
		t.Errorf("code = %d, want other", code)
	}
}

// retrieveSecret calls RetrieveSecret and returns what it wrote.
func (r *rig) retrieveSecret(t *testing.T, appID string) (uint32, []byte) {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	code, _ := r.interactive(t, "org.freedesktop.impl.portal.Secret.RetrieveSecret", handle, appID, dbus.UnixFD(pw.Fd()), Vardict{})
	_ = pw.Close()
	got, _ := io.ReadAll(pr)
	return code, got
}

func TestSecretIsStablePerApp(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	r := newRig(t, nil)
	code, first := r.retrieveSecret(t, "org.app")
	if code != ResponseSuccess || len(first) != secretLen {
		t.Fatalf("first = %d, %d bytes", code, len(first))
	}
	if _, again := r.retrieveSecret(t, "org.app"); !bytes.Equal(first, again) {
		t.Error("the secret changed between calls")
	}
	if _, other := r.retrieveSecret(t, "org.other"); bytes.Equal(first, other) {
		t.Error("two apps share a secret")
	}
	path := filepath.Join(data, "wayle/portal/secrets/org.app")
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("stored secret mode = %v, %v", st, err)
	}
	// A stored secret of the wrong length is replaced.
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, fresh := r.retrieveSecret(t, "org.app"); len(fresh) != secretLen || bytes.Equal(fresh, first) {
		t.Errorf("a short secret was kept: %d bytes", len(fresh))
	}
}

func TestSecretFailsWithoutAHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "relative")
	t.Setenv("HOME", "")
	_ = os.Unsetenv("HOME")
	r := newRig(t, nil)
	if code, got := r.retrieveSecret(t, "org.app"); code != ResponseOther || len(got) != 0 {
		t.Errorf("no home = %d, %d bytes", code, len(got))
	}
}

func TestSanitizeAppID(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "_.._.._etc_passwd", "..": "_..", ".": "_.", "": "_", "/": "_",
		"org.gnome.Builder": "org.gnome.Builder", "com.example_app-1": "com.example_app-1", "é": "_",
	} {
		if got := sanitizeAppID(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
