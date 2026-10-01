package dbusx_test

import (
	"errors"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/internal/dbusx"
)

type echo struct{}

func (echo) Echo(s string) (string, *dbus.Error) { return s, nil }

func (echo) Fail() *dbus.Error { return dbusx.Failed("it broke") }

const (
	name  = "com.wayle.Test1"
	iface = "com.wayle.Test1"
	path  = dbus.ObjectPath("/com/wayle/Test")
)

func serve(t *testing.T, conn *dbus.Conn, count *uint32) func() {
	t.Helper()
	release, err := dbusx.Serve(conn, dbusx.Service{
		Name: name, Path: path, Interface: iface, Methods: echo{},
		Properties: dbusx.Getters{"Count": func() any { return *count }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return release
}

func TestServeMethodsAndLiveProperties(t *testing.T) {
	dbustest.Session(t)
	count := uint32(1)
	defer serve(t, dbustest.SessionConn(t), &count)()

	obj := dbustest.SessionConn(t).Object(name, path)
	var out string
	if err := obj.Call(iface+".Echo", 0, "hi").Store(&out); err != nil || out != "hi" {
		t.Fatalf("Echo = %q, %v", out, err)
	}
	err := obj.Call(iface+".Fail", 0).Err
	var de dbus.Error
	if !errors.As(err, &de) || de.Name != dbusx.ErrFailed || de.Body[0] != "it broke" {
		t.Fatalf("Fail = %#v", err)
	}

	// Getters are computed per read, like zbus property getters.
	for _, want := range []uint32{1, 5} {
		count = want
		v, err := obj.GetProperty(iface + ".Count")
		if err != nil || v.Value() != want {
			t.Fatalf("Count = %v, %v; want %d", v, err, want)
		}
	}
	var all map[string]dbus.Variant
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, iface).Store(&all); err != nil || all["Count"].Value() != uint32(5) {
		t.Fatalf("GetAll = %v, %v", all, err)
	}
}

func TestServeRejectsUnknownAndWrites(t *testing.T) {
	dbustest.Session(t)
	count := uint32(0)
	defer serve(t, dbustest.SessionConn(t), &count)()
	obj := dbustest.SessionConn(t).Object(name, path)

	if _, err := obj.GetProperty(iface + ".Nope"); !isDBusError(err, dbusx.ErrUnknownProperty) {
		t.Errorf("unknown property: %v", err)
	}
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, "com.other", "Count").Err; !isDBusError(err, dbusx.ErrUnknownIface) {
		t.Errorf("unknown interface: %v", err)
	}
	if err := obj.SetProperty(iface+".Count", dbus.MakeVariant(uint32(3))); !isDBusError(err, dbusx.ErrPropertyRO) {
		t.Errorf("write: %v", err)
	}
}

func TestServeRefusesAnOwnedName(t *testing.T) {
	dbustest.Session(t)
	count := uint32(0)
	defer serve(t, dbustest.SessionConn(t), &count)()
	_, err := dbusx.Serve(dbustest.SessionConn(t), dbusx.Service{
		Name: name, Path: path, Interface: iface, Methods: echo{}, Properties: dbusx.Getters{},
	})
	if err == nil {
		t.Fatal("a second owner was accepted")
	}
}

func isDBusError(err error, name string) bool {
	var de dbus.Error
	return errors.As(err, &de) && de.Name == name
}
