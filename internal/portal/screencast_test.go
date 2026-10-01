package portal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeScreenCast is a portal answering every request with a Response
// signal on the spec's request path.
type fakeScreenCast struct {
	conn *dbus.Conn
	// cancelAt names the method whose request the user cancels.
	cancelAt string
	// gotSelect records SelectSources' options.
	gotSelect dbustest.Var[map[string]dbus.Variant]
	closed    dbustest.Var[int]
	remote    uintptr
}

func (f *fakeScreenCast) respond(sender dbus.Sender, method string, options map[string]dbus.Variant, results map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	handle, _ := options["handle_token"].Value().(string)
	path := dbus.ObjectPath(requestPrefix + strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_") + "/" + handle)
	code := ResponseSuccess
	if method == f.cancelAt {
		code = ResponseCancelled
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = f.conn.Emit(path, responseSig, code, results)
	}()
	return path, nil
}

func (f *fakeScreenCast) CreateSession(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return f.respond(sender, "CreateSession", options, map[string]dbus.Variant{
		"session_handle": dbus.MakeVariant("/org/freedesktop/portal/desktop/session/x/s1"),
	})
}

func (f *fakeScreenCast) SelectSources(sender dbus.Sender, _ dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	f.gotSelect.Store(options)
	return f.respond(sender, "SelectSources", options, map[string]dbus.Variant{})
}

type streamEntry struct {
	Node  uint32
	Props map[string]dbus.Variant
}

func (f *fakeScreenCast) Start(sender dbus.Sender, _ dbus.ObjectPath, _ string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return f.respond(sender, "Start", options, map[string]dbus.Variant{
		"streams": dbus.MakeVariant([]streamEntry{{Node: 42, Props: map[string]dbus.Variant{
			"size": dbus.MakeVariant(struct{ W, H int32 }{2560, 1440}),
		}}}),
		"restore_token": dbus.MakeVariant("tok-2"),
	})
}

func (f *fakeScreenCast) OpenPipeWireRemote(_ dbus.ObjectPath, _ map[string]dbus.Variant) (dbus.UnixFD, *dbus.Error) {
	return dbus.UnixFD(f.remote), nil
}

type fakeSession struct{ f *fakeScreenCast }

func (s fakeSession) Close() *dbus.Error {
	s.f.closed.Update(func(n int) int { return n + 1 })
	return nil
}

func serveScreenCast(t *testing.T, bus *dbustest.Bus, cancelAt string) *fakeScreenCast {
	t.Helper()
	conn := bus.Conn(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	f := &fakeScreenCast{conn: conn, remote: r.Fd(), cancelAt: cancelAt}
	if err := conn.Export(f, ObjectPath, ScreenCastIface); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(fakeSession{f}, "/org/freedesktop/portal/desktop/session/x/s1", SessionIface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", BusName, reply, err)
	}
	return f
}

func TestOpenScreenCastNegotiatesAndKeepsTheToken(t *testing.T) {
	bus := dbustest.Start(t)
	f := serveScreenCast(t, bus, "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, restoreTokenFile), []byte("tok-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := OpenScreenCast(ctx, bus.Conn(t), true, dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Node != 42 || s.Width != 2560 || s.Height != 1440 || s.Remote == nil {
		t.Errorf("stream = %+v", s)
	}
	opts := f.gotSelect.Load()
	if opts["cursor_mode"].Value() != CursorEmbedded || opts["types"].Value() != SourceMonitor ||
		opts["persist_mode"].Value() != PersistUntilRevoked || opts["restore_token"].Value() != "tok-1" {
		t.Errorf("SelectSources options = %v", opts)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, restoreTokenFile)); string(body) != "tok-2" {
		t.Errorf("saved token = %q, want the refreshed one", body)
	}
	s.Close()
	waitFor(t, func() bool { return f.closed.Load() == 1 })
}

func TestOpenScreenCastHiddenCursorWithoutToken(t *testing.T) {
	bus := dbustest.Start(t)
	f := serveScreenCast(t, bus, "")
	s, err := OpenScreenCast(context.Background(), bus.Conn(t), false, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	opts := f.gotSelect.Load()
	if opts["cursor_mode"].Value() != CursorHidden {
		t.Errorf("cursor = %v, want hidden", opts["cursor_mode"])
	}
	if _, ok := opts["restore_token"]; ok {
		t.Error("a restore token was sent without a state dir")
	}
}

func TestOpenScreenCastCancelledPickerClosesTheSession(t *testing.T) {
	bus := dbustest.Start(t)
	f := serveScreenCast(t, bus, "SelectSources")
	_, err := OpenScreenCast(context.Background(), bus.Conn(t), true, t.TempDir())
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want cancelled", err)
	}
	waitFor(t, func() bool { return f.closed.Load() == 1 })
}

func TestOpenScreenCastWithoutPortal(t *testing.T) {
	bus := dbustest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := OpenScreenCast(ctx, bus.Conn(t), true, ""); err == nil {
		t.Error("no portal on the bus: want an error")
	}
}

func TestReadStreamRejectsEmpty(t *testing.T) {
	var s Stream
	if err := s.readStream(map[string]dbus.Variant{}); err == nil {
		t.Error("no streams: want an error")
	}
	if err := s.readStream(map[string]dbus.Variant{"streams": dbus.MakeVariant([][]any{})}); err == nil {
		t.Error("empty streams: want an error")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fakeChooser answers OpenFile with uri, recording the filters.
type fakeChooser struct {
	conn    *dbus.Conn
	uri     string
	cancel  bool
	filters dbustest.Var[[]filterEntry]
	title   dbustest.Var[string]
}

func (f *fakeChooser) OpenFile(sender dbus.Sender, _ string, title string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	f.title.Store(title)
	var entries []filterEntry
	if v, ok := options["filters"]; ok {
		_ = v.Store(&entries)
	}
	f.filters.Store(entries)
	fs := &fakeScreenCast{conn: f.conn}
	if f.cancel {
		fs.cancelAt = "OpenFile"
	}
	return fs.respond(sender, "OpenFile", options, map[string]dbus.Variant{"uris": dbus.MakeVariant([]string{f.uri})})
}

func serveChooser(t *testing.T, bus *dbustest.Bus, uri string, cancel bool) *fakeChooser {
	t.Helper()
	conn := bus.Conn(t)
	f := &fakeChooser{conn: conn, uri: uri, cancel: cancel}
	if err := conn.Export(f, ObjectPath, FileChooserIface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own: %v %v", reply, err)
	}
	return f
}

func TestOpenFile(t *testing.T) {
	bus := dbustest.Start(t)
	f := serveChooser(t, bus, "file:///home/u/wg%200.conf", false)
	path, err := OpenFile(context.Background(), bus.Conn(t), "Import", FileFilter{Name: "WireGuard", Patterns: []string{"*.conf"}})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/home/u/wg 0.conf" {
		t.Errorf("path = %q", path)
	}
	if got := f.filters.Load(); len(got) != 1 || got[0].Name != "WireGuard" || len(got[0].Rules) != 1 || got[0].Rules[0].Pattern != "*.conf" {
		t.Errorf("filters = %+v", got)
	}
	if f.title.Load() != "Import" {
		t.Errorf("title = %q", f.title.Load())
	}
}

func TestOpenFileCancelledAndRemote(t *testing.T) {
	bus := dbustest.Start(t)
	serveChooser(t, bus, "file:///x", true)
	if _, err := OpenFile(context.Background(), bus.Conn(t), "Import"); !errors.Is(err, ErrCancelled) {
		t.Errorf("cancelled = %v", err)
	}
	bus2 := dbustest.Start(t)
	serveChooser(t, bus2, "https://example.com/x.conf", false)
	if _, err := OpenFile(context.Background(), bus2.Conn(t), "Import"); err == nil {
		t.Error("a remote uri was accepted")
	}
}
