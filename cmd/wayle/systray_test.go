package main

import (
	"context"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/sni"
)

type fakeActivator struct {
	got dbustest.Var[[]string]
	err error
}

func (f *fakeActivator) Activate(_ context.Context, it sni.Item, x, y int32) error {
	if x != 0 || y != 0 {
		panic("activation must be at (0, 0)")
	}
	f.got.Update(func(ids []string) []string { return append(ids, it.ID) })
	return f.err
}

func serveTray(t *testing.T, act *fakeActivator, items ...*sni.Item) {
	t.Helper()
	store := sni.NewStore()
	for _, it := range items {
		store.Put(it)
	}
	release, err := sni.ServeDaemon(dbustest.SessionConn(t), store, act, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
}

func TestSystrayCommands(t *testing.T) {
	dbustest.Session(t)
	act := &fakeActivator{}
	serveTray(t, act,
		&sni.Item{Bus: ":1.2", Path: "/a", ID: "nm-applet", Title: "Network", IconName: "nm-signal-75", Status: sni.StatusActive},
		&sni.Item{Bus: ":1.3", Path: "/b", ID: "steam", Title: "steam", Status: sni.StatusPassive},
	)
	stdout, stderr, code := runCaptured(t, false, "systray", "list")
	want := "System tray items:\n  nm-applet \"Network\" (nm-signal-75) [Active]\n  steam [Passive]\n"
	if code != 0 || stdout != want {
		t.Errorf("list: code %d %q %q", code, stdout, stderr)
	}
	stdout, _, _ = runCaptured(t, false, "systray", "status")
	if stdout != "Tray items: 2\nStatusNotifierWatcher: active\n" {
		t.Errorf("status: %q", stdout)
	}
	stdout, _, code = runCaptured(t, false, "systray", "activate", "steam")
	if code != 0 || stdout != "Activated: steam\n" || !slices.Equal(act.got.Load(), []string{"steam"}) {
		t.Errorf("activate: code %d %q %v", code, stdout, act.got.Load())
	}
}

func TestSystrayErrors(t *testing.T) {
	dbustest.Session(t)
	if _, stderr, code := runCaptured(t, false, "systray", "list"); code != 1 || stderr != "Error: System tray service not running. Start wayle shell first.\n" {
		t.Errorf("not running: code %d %q", code, stderr)
	}
	act := &fakeActivator{err: dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownMethod"}}
	serveTray(t, act, &sni.Item{Bus: ":1.2", Path: "/a", ID: "menu-only"})

	if stdout, _, _ := runCaptured(t, false, "systray", "list"); stdout != "System tray items:\n  menu-only [Passive]\n" {
		t.Errorf("empty status/title: %q", stdout)
	}
	if _, stderr, code := runCaptured(t, false, "systray", "activate", "ghost"); code != 1 || stderr != "Error: Failed to activate tray item: Tray item not found: ghost\n" {
		t.Errorf("unknown id: code %d %q", code, stderr)
	}
	if _, stderr, _ := runCaptured(t, false, "systray", "activate", "menu-only"); stderr != "Error: Failed to activate tray item: tray item does not support 'activate', use its menu instead\n" {
		t.Errorf("menu-only item: %q", stderr)
	}
}

func TestSystrayEmpty(t *testing.T) {
	dbustest.Session(t)
	serveTray(t, &fakeActivator{})
	if stdout, _, code := runCaptured(t, false, "systray", "list"); code != 0 || stdout != "No system tray items\n" {
		t.Errorf("empty: code %d %q", code, stdout)
	}
}
