package portal

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeShortcuts is the compositor's shortcut manager.
type fakeShortcuts struct {
	onEvent      dbustest.Var[func(string, bool, uint64)]
	registered   dbustest.Var[[]string]
	unregistered dbustest.Var[[]string]
}

func (f *fakeShortcuts) register(key, id, appID, description, trigger string) (func(), error) {
	f.registered.Update(func(r []string) []string { return append(r, appID+"/"+id+"|"+description+"|"+trigger) })
	return func() {
		f.unregistered.Update(func(u []string) []string { return append(u, key) })
	}, nil
}

func shortcutsRig(t *testing.T, startErr error) (*rig, *fakeShortcuts) {
	f := &fakeShortcuts{}
	r := newRig(t, func(b *Backend) {
		b.shortcuts = newGlobalShortcuts(b.conn, b.sessions, func(on func(string, bool, uint64)) (shortcutDevice, error) {
			if startErr != nil {
				return nil, startErr
			}
			f.onEvent.Store(on)
			return f, nil
		})
	})
	return r, f
}

func props(desc string) Vardict { return Vardict{"description": dbus.MakeVariant(desc)} }

func (r *rig) listShortcuts(t *testing.T, session dbus.ObjectPath) []shortcut {
	t.Helper()
	_, res := r.interactive(t, GlobalShortcutsIface+".ListShortcuts", handle, session)
	var got []shortcut
	if err := dbus.Store([]any{res["shortcuts"].Value()}, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func shortcutIDs(list []shortcut) []string {
	var ids []string
	for _, s := range list {
		ids = append(ids, s.ID+"="+stringOr(s.Props, "description", ""))
	}
	return ids
}

func (r *rig) shortcutSignal(t *testing.T) *dbus.Signal {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case s := <-r.signals:
			if s.Name == GlobalShortcutsIface+".Activated" || s.Name == GlobalShortcutsIface+".Deactivated" {
				return s
			}
		case <-deadline:
			return nil
		}
	}
}

func TestGlobalShortcutsBindAndActivate(t *testing.T) {
	r, f := shortcutsRig(t, nil)
	opts := Vardict{"shortcuts": dbus.MakeVariant([]shortcut{
		{"mic", Vardict{"description": dbus.MakeVariant("Toggle mic"), "preferred_trigger": dbus.MakeVariant("CTRL+M")}},
	})}
	if code, _ := r.interactive(t, GlobalShortcutsIface+".CreateSession", handle, sessA, "org.app", opts); code != ResponseSuccess {
		t.Fatalf("CreateSession = %d", code)
	}
	if got := f.registered.Load(); !slices.Equal(got, []string{"org.app/mic|Toggle mic|CTRL+M"}) {
		t.Errorf("registered %v", got)
	}

	// A re-bind of mic updates its props without a second registration.
	_, res := r.interactive(t, GlobalShortcutsIface+".BindShortcuts", handle, sessA, []shortcut{{"mic", props("Mute")}, {"next", props("Next")}}, "", Vardict{})
	var bound []shortcut
	_ = dbus.Store([]any{res["shortcuts"].Value()}, &bound)
	if got := shortcutIDs(bound); !slices.Equal(got, []string{"mic=Mute", "next=Next"}) {
		t.Errorf("bound %v", got)
	}
	if got := f.registered.Load(); len(got) != 2 || got[1] != "org.app/next|Next|" {
		t.Errorf("registered %v", got)
	}
	if got := shortcutIDs(r.listShortcuts(t, sessA)); !slices.Equal(got, []string{"mic=Mute", "next=Next"}) {
		t.Errorf("listed %v", got)
	}

	on := f.onEvent.Load()
	on(shortcutKey("org.app", "next"), true, 42)
	if s := r.shortcutSignal(t); s == nil || s.Name != GlobalShortcutsIface+".Activated" || s.Body[0] != sessA || s.Body[1] != "next" || s.Body[2] != uint64(42) {
		t.Fatalf("press = %v", s)
	}
	on(shortcutKey("org.app", "next"), false, 43)
	if s := r.shortcutSignal(t); s == nil || s.Name != GlobalShortcutsIface+".Deactivated" || s.Body[2] != uint64(43) {
		t.Fatalf("release = %v", s)
	}
	on(shortcutKey("org.other", "next"), true, 1)
	if s := r.shortcutSignal(t); s != nil {
		t.Errorf("an unregistered key signalled %v", s)
	}

	// Closing the session unregisters its shortcuts; their keys go quiet.
	if err := r.client.Object(BusName, sessA).Call(SessionIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	if got := f.unregistered.Load(); len(got) != 2 {
		t.Errorf("unregistered %v", got)
	}
	on(shortcutKey("org.app", "mic"), true, 2)
	if s := r.shortcutSignal(t); s != nil {
		t.Errorf("a closed session's key signalled %v", s)
	}
	if got := r.listShortcuts(t, sessA); len(got) != 0 {
		t.Errorf("a closed session lists %v", got)
	}
}

func TestGlobalShortcutsWithoutTheProtocol(t *testing.T) {
	r, _ := shortcutsRig(t, errors.New("no hyprland_global_shortcuts_manager_v1"))
	if code, _ := r.interactive(t, GlobalShortcutsIface+".CreateSession", handle, sessA, "org.app", Vardict{"shortcuts": dbus.MakeVariant("garbled")}); code != ResponseSuccess {
		t.Fatalf("CreateSession = %d", code)
	}
	if got := r.listShortcuts(t, sessA); len(got) != 0 {
		t.Errorf("garbled shortcuts listed %v", got)
	}
	// Binds are accepted and kept; nothing will ever activate them.
	code, _ := r.interactive(t, GlobalShortcutsIface+".BindShortcuts", handle, sessA, []shortcut{{"mic", props("Mic")}}, "", Vardict{})
	if got := shortcutIDs(r.listShortcuts(t, sessA)); code != ResponseSuccess || !slices.Equal(got, []string{"mic=Mic"}) {
		t.Errorf("bind = %d, listed %v", code, got)
	}
}

func TestShortcutKeySeparates(t *testing.T) {
	if shortcutKey("a", "b") == shortcutKey("ab", "") {
		t.Error("distinct pairs share a key")
	}
}
