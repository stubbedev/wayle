package portal

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/internal/dbusx"
)

// rig is a backend serving on a private bus, with a client connection
// that calls it the way the frontend does.
type rig struct {
	cfg     *config.Service
	backend *Backend
	client  *dbus.Conn
	obj     dbus.BusObject
	signals chan *dbus.Signal
}

func newRig(t *testing.T, edit func(*Backend)) *rig {
	t.Helper()
	bus := dbustest.Start(t)
	cfg := config.Load(t.TempDir(), config.DiscardDiagnostics)
	client := bus.Conn(t)
	signals := make(chan *dbus.Signal, 64)
	if err := client.AddMatchSignal(dbus.WithMatchObjectPath(ObjectPath)); err != nil {
		t.Fatal(err)
	}
	client.Signal(signals)
	b := New(bus.Conn(t), cfg)
	if edit != nil {
		edit(b)
	}
	stop, err := b.Serve()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return &rig{cfg: cfg, backend: b, client: client, obj: client.Object(BusName, ObjectPath), signals: signals}
}

// changed collects the SettingChanged keys emitted within a short wait.
func (r *rig) changed(t *testing.T) []string {
	t.Helper()
	var keys []string
	timeout := time.After(300 * time.Millisecond)
	for {
		select {
		case s := <-r.signals:
			if s.Name != SettingsIface+".SettingChanged" {
				continue
			}
			if s.Body[0] != AppearanceNS {
				t.Errorf("namespace = %v", s.Body[0])
			}
			keys = append(keys, s.Body[1].(string))
		case <-timeout:
			return keys
		}
	}
}

func (r *rig) readOne(ns, key string) (any, error) {
	var v dbus.Variant
	err := r.obj.Call(SettingsIface+".ReadOne", 0, ns, key).Store(&v)
	return v.Value(), err
}

func TestSettingsReadAllServesAppearance(t *testing.T) {
	r := newRig(t, nil)
	var all map[string]map[string]dbus.Variant
	if err := r.obj.Call(SettingsIface+".ReadAll", 0, []string{}).Store(&all); err != nil {
		t.Fatal(err)
	}
	got := all[AppearanceNS]
	// The default palette is the dark wayle theme with a #e0947a accent.
	if got[keyColorScheme].Value() != uint32(1) || got[keyContrast].Value() != uint32(0) || got[keyReducedMotion].Value() != uint32(0) {
		t.Errorf("appearance = %v", got)
	}
	// The accent is the spec's (ddd) structure, not an array.
	var acc dbus.Variant
	if err := r.obj.Call(SettingsIface+".ReadOne", 0, AppearanceNS, keyAccentColor).Store(&acc); err != nil || acc.Signature().String() != "(ddd)" {
		t.Errorf("accent = %v (%s), %v; want (ddd)", acc, acc.Signature(), err)
	}
	if want := []any{0xe0 / 255.0, 0x94 / 255.0, 0x7a / 255.0}; !slices.Equal(acc.Value().([]any), want) {
		t.Errorf("accent = %v, want %v", acc.Value(), want)
	}
	// A prefix of the namespace selects it; another namespace does not.
	all = nil
	if err := r.obj.Call(SettingsIface+".ReadAll", 0, []string{"org.freedesktop.app"}).Store(&all); err != nil || len(all[AppearanceNS]) != 4 {
		t.Errorf("prefix ReadAll = %v, %v", all, err)
	}
	all = nil
	if err := r.obj.Call(SettingsIface+".ReadAll", 0, []string{"org.gnome.desktop"}).Store(&all); err != nil || len(all) != 0 {
		t.Errorf("foreign ReadAll = %v, %v", all, err)
	}
}

func TestSettingsReadOneAndUnknownKeys(t *testing.T) {
	r := newRig(t, nil)
	if v, err := r.readOne(AppearanceNS, keyColorScheme); err != nil || v != uint32(1) {
		t.Errorf("ReadOne color-scheme = %v, %v", v, err)
	}
	var v dbus.Variant
	if err := r.obj.Call(SettingsIface+".Read", 0, AppearanceNS, keyReducedMotion).Store(&v); err != nil || v.Value() != uint32(0) {
		t.Errorf("Read reduced-motion = %v, %v", v, err)
	}
	for _, tc := range [][2]string{{AppearanceNS, "font-name"}, {"org.gnome.desktop.interface", keyColorScheme}} {
		_, err := r.readOne(tc[0], tc[1])
		var de dbus.Error
		if !errors.As(err, &de) || de.Name != dbusx.ErrFailed || de.Body[0] != "unknown setting "+tc[0]+"/"+tc[1] {
			t.Errorf("ReadOne %s/%s = %v", tc[0], tc[1], err)
		}
	}
}

func TestColorScheme(t *testing.T) {
	s := config.DefaultsStyling()
	if colorScheme(s) != 1 {
		t.Error("auto over a dark background is not dark")
	}
	s.Palette.Bg, _ = config.ParseHexColor("#f0f0f0")
	if colorScheme(s) != 2 {
		t.Error("auto over a light background is not light")
	}
	s.Appearance = config.AppearanceDark
	if colorScheme(s) != 1 {
		t.Error("forced dark is not dark")
	}
	s.Appearance = config.AppearanceLight
	s.Palette.Bg, _ = config.ParseHexColor("#000")
	if colorScheme(s) != 2 {
		t.Error("forced light is not light")
	}
}

func TestSettingChangedFollowsTheConfig(t *testing.T) {
	r := newRig(t, nil)
	// Each followed source emits its current value once at startup.
	if got, want := r.changed(t), []string{keyColorScheme, keyColorScheme, keyAccentColor, keyContrast, keyReducedMotion}; !slices.Equal(got, want) {
		t.Fatalf("startup emissions = %v, want %v", got, want)
	}
	if err := r.cfg.SetByPath("animations.enabled", false); err != nil {
		t.Fatal(err)
	}
	if got := r.changed(t); !slices.Equal(got, []string{keyReducedMotion}) {
		t.Errorf("animations off emitted %v", got)
	}
	if v, _ := r.readOne(AppearanceNS, keyReducedMotion); v != uint32(1) {
		t.Errorf("reduced-motion = %v after animations off", v)
	}
	if err := r.cfg.SetByPath("styling.appearance", "light"); err != nil {
		t.Fatal(err)
	}
	if got := r.changed(t); !slices.Equal(got, []string{keyColorScheme}) {
		t.Errorf("appearance light emitted %v", got)
	}
	// A setting the portal does not serve emits nothing.
	if err := r.cfg.SetByPath("styling.scale", 1.5); err != nil {
		t.Fatal(err)
	}
	if got := r.changed(t); len(got) != 0 {
		t.Errorf("an unserved setting emitted %v", got)
	}
}

func TestLockdownLocksNothing(t *testing.T) {
	r := newRig(t, nil)
	var all map[string]dbus.Variant
	if err := r.obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.impl.portal.Lockdown").Store(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 || all["version"].Value() != uint32(1) {
		t.Fatalf("lockdown = %v", all)
	}
	for name, v := range all {
		if name != "version" && v.Value() != false {
			t.Errorf("%s = %v", name, v)
		}
	}
}

func TestServeRefusesAnOwnedName(t *testing.T) {
	bus := dbustest.Start(t)
	other := bus.Conn(t)
	if _, err := other.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if _, err := New(bus.Conn(t), config.Load(t.TempDir(), config.DiscardDiagnostics)).Serve(); err == nil {
		t.Error("a second backend took an owned name")
	}
}

func TestRequestCloseCancels(t *testing.T) {
	bus := dbustest.Start(t)
	server, client := bus.Conn(t), bus.Conn(t)
	handle := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_1/t")
	req, err := mountRequest(server, handle)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-req.Cancelled():
		t.Fatal("cancelled before Close")
	default:
	}
	obj := client.Object(server.Names()[0], handle)
	if err := obj.Call(RequestIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-req.Cancelled():
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel")
	}
	// A second Close is harmless; after end the object is gone.
	if err := obj.Call(RequestIface+".Close", 0).Err; err != nil {
		t.Error(err)
	}
	req.end()
	if err := obj.Call(RequestIface+".Close", 0).Err; err == nil {
		t.Error("the Request outlived its call")
	}
}

func TestSessionCloseRunsCleanupOnce(t *testing.T) {
	bus := dbustest.Start(t)
	server, client := bus.Conn(t), bus.Conn(t)
	reg := newSessions(server)
	var closes dbustest.Var[int]
	first, second := dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_1/a"), dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_1/b")
	for _, p := range []dbus.ObjectPath{first, second} {
		if err := reg.mount(p, func() { closes.Update(func(n int) int { return n + 1 }) }); err != nil {
			t.Fatal(err)
		}
	}
	signals := make(chan *dbus.Signal, 4)
	_ = client.AddMatchSignal(dbus.WithMatchInterface(SessionIface))
	client.Signal(signals)

	obj := client.Object(server.Names()[0], first)
	if v, err := obj.GetProperty(SessionIface + ".version"); err != nil || v.Value() != uint32(2) {
		t.Errorf("version = %v, %v", v, err)
	}
	if err := obj.Call(SessionIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-signals:
		if s.Path != first || s.Name != SessionIface+".Closed" {
			t.Errorf("signal = %v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("Close emitted no Closed")
	}
	if closes.Load() != 1 {
		t.Errorf("cleanups after Close = %d", closes.Load())
	}
	if err := obj.Call(SessionIface+".Close", 0).Err; err == nil {
		t.Error("a closed session still answers")
	}
	// Shutdown cleans up the live session only, and only once.
	reg.clearAll()
	reg.clearAll()
	if closes.Load() != 2 {
		t.Errorf("cleanups after shutdown = %d, want 2", closes.Load())
	}
	// The app closing a session shutdown already cleaned up runs nothing.
	if err := client.Object(server.Names()[0], second).Call(SessionIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	if closes.Load() != 2 {
		t.Errorf("cleanups after a late Close = %d, want 2", closes.Load())
	}
}

func TestStore(t *testing.T) {
	s := newStore[int]()
	p := dbus.ObjectPath("/s/1")
	s.set(p, 7)
	s.update(p, func(v *int) { *v++ })
	if v, ok := s.get(p); !ok || v != 8 {
		t.Errorf("get = %d, %v", v, ok)
	}
	s.update("/s/missing", func(*int) { t.Error("update ran for a missing session") })
	if v, ok := s.remove(p); !ok || v != 8 {
		t.Errorf("remove = %d, %v", v, ok)
	}
	if _, ok := s.get(p); ok {
		t.Error("removed state still present")
	}
}

func TestOptions(t *testing.T) {
	o := Vardict{
		"u32": dbus.MakeVariant(uint32(7)), "u64": dbus.MakeVariant(uint64(9)), "big": dbus.MakeVariant(uint64(1) << 40),
		"b": dbus.MakeVariant(true), "s": dbus.MakeVariant("hi"), "wrong": dbus.MakeVariant(int32(3)),
	}
	for key, want := range map[string]uint32{"u32": 7, "u64": 9} {
		if v, ok := optU32(o, key); !ok || v != want {
			t.Errorf("optU32 %s = %d, %v", key, v, ok)
		}
	}
	for _, key := range []string{"big", "wrong", "missing"} {
		if _, ok := optU32(o, key); ok {
			t.Errorf("optU32 %s read a value", key)
		}
	}
	if v, ok := optBool(o, "b"); !ok || !v {
		t.Error("optBool")
	}
	if _, ok := optBool(o, "s"); ok {
		t.Error("optBool read a string")
	}
	if stringOr(o, "s", "x") != "hi" || stringOr(o, "missing", "x") != "x" || stringOr(o, "b", "x") != "x" {
		t.Error("stringOr")
	}
}
