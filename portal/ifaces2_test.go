package portal

import (
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/shell/portaldialogs"
)

// fakeNotifyd is the shell's notification daemon.
type fakeNotifyd struct {
	conn   *dbus.Conn
	next   uint32
	notify dbustest.Var[[]any]
	closed dbustest.Var[[]uint32]
}

func (f *fakeNotifyd) Notify(appName string, replaces uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, expire int32) (uint32, *dbus.Error) {
	f.notify.Store([]any{appName, replaces, icon, summary, body, actions, len(hints), expire})
	f.next++
	return f.next, nil
}

func (f *fakeNotifyd) CloseNotification(id uint32) *dbus.Error {
	f.closed.Update(func(ids []uint32) []uint32 { return append(ids, id) })
	return nil
}

func serveNotifyd(t *testing.T, r *rig) *fakeNotifyd {
	t.Helper()
	f := &fakeNotifyd{conn: r.bus.Conn(t), next: 40}
	if err := f.conn.Export(f, notifications.ObjPath, notifications.Interface); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.RequestName(notifications.Interface, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return f
}

// gicon is a serialized GIcon, (sv).
type gicon struct {
	Kind string
	Data dbus.Variant
}

const notificationAdd = NotificationIface + ".AddNotification"

func TestNotificationShowsAndForwardsActions(t *testing.T) {
	r := newRig(t, nil)
	d := serveNotifyd(t, r)
	n := Vardict{
		"title":          dbus.MakeVariant("Hello"),
		"body":           dbus.MakeVariant("World"),
		"icon":           dbus.MakeVariant(gicon{"themed", dbus.MakeVariant([]string{"mail-unread", "mail-unread-symbolic"})}),
		"default-action": dbus.MakeVariant("open"),
		"buttons": dbus.MakeVariant([]map[string]dbus.Variant{
			{"label": dbus.MakeVariant("Reply"), "action": dbus.MakeVariant("reply")},
			{"label": dbus.MakeVariant("No action")},
		}),
	}
	if err := r.obj.Call(notificationAdd, 0, "org.app", "n1", n).Err; err != nil {
		t.Fatal(err)
	}
	want := []any{"org.app", uint32(0), "mail-unread", "Hello", "World", []string{"open", "Default", "reply", "Reply"}, 0, int32(-1)}
	got := d.notify.Load()
	if len(got) != len(want) || !slices.Equal(got[5].([]string), want[5].([]string)) {
		t.Fatalf("Notify = %v, want %v", got, want)
	}
	for i := range want {
		if i != 5 && got[i] != want[i] {
			t.Errorf("Notify arg %d = %v, want %v", i, got[i], want[i])
		}
	}

	// The daemon's press on our notification reaches the app; one on a
	// notification the portal did not create does not.
	_ = d.conn.Emit(notifications.ObjPath, notifications.Interface+".ActionInvoked", uint32(99), "x")
	_ = d.conn.Emit(notifications.ObjPath, notifications.Interface+".ActionInvoked", uint32(41), "reply")
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-r.signals:
			if s.Name != NotificationIface+".ActionInvoked" {
				continue
			}
			if s.Body[0] != "org.app" || s.Body[1] != "n1" || s.Body[2] != "reply" {
				t.Fatalf("ActionInvoked = %v", s.Body)
			}
		case <-deadline:
			t.Fatal("no ActionInvoked forwarded")
		}
		break
	}

	// Remove closes the daemon's notification once; an unknown one is
	// left alone.
	for range 2 {
		if err := r.obj.Call(NotificationIface+".RemoveNotification", 0, "org.app", "n1").Err; err != nil {
			t.Fatal(err)
		}
	}
	_ = r.obj.Call(NotificationIface+".RemoveNotification", 0, "org.other", "n1").Err
	if got := d.closed.Load(); !slices.Equal(got, []uint32{41}) {
		t.Errorf("closed = %v, want [41]", got)
	}
}

func TestNotificationDefaults(t *testing.T) {
	r := newRig(t, nil)
	d := serveNotifyd(t, r)
	if err := r.obj.Call(notificationAdd, 0, "org.app", "n1", Vardict{"icon": dbus.MakeVariant(gicon{"file", dbus.MakeVariant("/x.png")})}).Err; err != nil {
		t.Fatal(err)
	}
	if got := d.notify.Load(); got[3] != "org.app" || got[4] != "" || got[2] != "" || len(got[5].([]string)) != 0 {
		t.Errorf("Notify = %v: the app id titles it, no body, no icon, no actions", got)
	}
	if got := notificationIcon(Vardict{"icon": dbus.MakeVariant("dialog-information")}); got != "dialog-information" {
		t.Errorf("bare icon = %q", got)
	}
}

// fakeWallpaperd is the shell's com.wayle.Wallpaper1.
type fakeWallpaperd struct{ set dbustest.Var[[]string] }

func (f *fakeWallpaperd) SetWallpaper(path, monitor string) *dbus.Error {
	f.set.Store([]string{path, monitor})
	return nil
}

// fakeDialogs answers every dialog with yes or no.
type fakeDialogs struct {
	yes   dbustest.Var[bool]
	asked dbustest.Var[[]any]
}

func (f *fakeDialogs) Access(r portaldialogs.AccessRequest) bool {
	f.asked.Store([]any{r})
	return f.yes.Load()
}

func (f *fakeDialogs) Account(reason string) bool { f.asked.Store([]any{reason}); return f.yes.Load() }

func (f *fakeDialogs) ConfirmWallpaper(uri string) bool {
	f.asked.Store([]any{uri})
	return f.yes.Load()
}

func (f *fakeDialogs) ChooseApplication(choices []string, contentType, uri string) string {
	f.asked.Store([]any{choices, contentType, uri})
	if f.yes.Load() {
		return "org.chosen.desktop"
	}
	return ""
}

func (f *fakeDialogs) ConfirmInstall(name, icon string) bool {
	f.asked.Store([]any{name, icon})
	return f.yes.Load()
}

func serveDialogs(t *testing.T, r *rig, yes bool) *fakeDialogs {
	t.Helper()
	f := &fakeDialogs{}
	f.yes.Store(yes)
	release, err := portaldialogs.NewDaemon(f).Export(r.bus.Conn(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return f
}

func serveWallpaperd(t *testing.T, r *rig) *fakeWallpaperd {
	t.Helper()
	f := &fakeWallpaperd{}
	conn := r.bus.Conn(t)
	if err := conn.Export(f, wallpaper.ServicePath, wallpaper.Interface); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(wallpaper.ServiceName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return f
}

func (r *rig) setWallpaper(t *testing.T, uri string, options Vardict) uint32 {
	t.Helper()
	var code uint32
	if err := r.obj.Call("org.freedesktop.impl.portal.Wallpaper.SetWallpaperURI", 0, handle, "org.app", "", uri, options).Store(&code); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestWallpaperSetsAFileURI(t *testing.T) {
	r := newRig(t, nil)
	w := serveWallpaperd(t, r)
	if code := r.setWallpaper(t, "file:///home/u/My%20Walls/a%23b.png", Vardict{}); code != ResponseSuccess {
		t.Errorf("code = %d", code)
	}
	if got := w.set.Load(); !slices.Equal(got, []string{"/home/u/My Walls/a#b.png", ""}) {
		t.Errorf("SetWallpaper = %q: the decoded path on every monitor", got)
	}
	if code := r.setWallpaper(t, "https://example.com/x.png", Vardict{}); code != ResponseOther {
		t.Errorf("https code = %d", code)
	}
}

func TestWallpaperPreview(t *testing.T) {
	r := newRig(t, nil)
	w := serveWallpaperd(t, r)
	preview := Vardict{"show-preview": dbus.MakeVariant(true)}
	// No dialog host: the request fails rather than applying unasked.
	if code := r.setWallpaper(t, "file:///a.png", preview); code != ResponseOther || w.set.Load() != nil {
		t.Errorf("no host: code %d, set %v", code, w.set.Load())
	}
	d := serveDialogs(t, r, false)
	if code := r.setWallpaper(t, "file:///a.png", preview); code != ResponseCancelled || w.set.Load() != nil {
		t.Errorf("declined: code %d, set %v", code, w.set.Load())
	}
	if got := d.asked.Load(); got[0] != "file:///a.png" {
		t.Errorf("previewed %v", got)
	}
	d.yes.Store(true)
	if code := r.setWallpaper(t, "file:///a.png", preview); code != ResponseSuccess || w.set.Load()[0] != "/a.png" {
		t.Errorf("accepted: code %d, set %v", code, w.set.Load())
	}
}

func TestWallpaperFailsWithoutTheShell(t *testing.T) {
	r := newRig(t, nil)
	if code := r.setWallpaper(t, "file:///a.png", Vardict{}); code != ResponseOther {
		t.Errorf("code = %d", code)
	}
}

func TestInhibitHoldsTheLockUntilClose(t *testing.T) {
	var asked dbustest.Var[[]string]
	var lockErr dbustest.Var[error]
	var held dbustest.Var[*os.File]
	r := newRig(t, func(b *Backend) {
		b.inhibitLock = func(what string) (*os.File, error) {
			asked.Update(func(w []string) []string { return append(w, what) })
			if err := lockErr.Load(); err != nil {
				return nil, err
			}
			pr, pw, err := os.Pipe()
			held.Store(pr)
			return pw, err
		}
	})
	const iface = "org.freedesktop.impl.portal.Inhibit.Inhibit"
	if err := r.obj.Call(iface, 0, handle, "org.app", "", uint32(inhibitIdle|inhibitSuspend|inhibitLogout), Vardict{}).Err; err != nil {
		t.Fatal(err)
	}
	if got := asked.Load(); !slices.Equal(got, []string{"idle:sleep:shutdown"}) {
		t.Fatalf("logind asked for %v", got)
	}
	req := r.client.Object(BusName, handle)
	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(held.Load())
		readDone <- err
	}()
	select {
	case <-readDone:
		t.Fatal("the lock was released before Close")
	case <-time.After(50 * time.Millisecond):
	}
	if err := req.Call(RequestIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("Close kept the lock")
	}
	if err := req.Call(RequestIface+".Close", 0).Err; err == nil {
		t.Error("the Request outlived its lock")
	}

	// No flags asks logind for nothing; a logind failure exports nothing.
	_ = r.obj.Call(iface, 0, handle, "org.app", "", uint32(2), Vardict{}).Err
	lockErr.Store(errors.New("no logind"))
	_ = r.obj.Call(iface, 0, handle, "org.app", "", uint32(inhibitIdle), Vardict{}).Err
	if got := asked.Load(); len(got) != 2 {
		t.Errorf("logind asked %v", got)
	}
	if err := req.Call(RequestIface+".Close", 0).Err; err == nil {
		t.Error("a failed lock exported a Request")
	}
}
