// Package dbusx holds the session-bus plumbing wayle's daemons share:
// exporting an interface under a well-known name the way zbus's
// object_server().at + request_name does, and serving zbus-style
// #[zbus(property)] getters (computed on every read) over
// org.freedesktop.DBus.Properties.
package dbusx

import (
	"fmt"
	"sort"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// Error names the Rust daemons reply with (zbus fdo::Error variants).
const (
	ErrFailed          = "org.freedesktop.DBus.Error.Failed"
	ErrInvalidArgs     = "org.freedesktop.DBus.Error.InvalidArgs"
	ErrUnknownProperty = "org.freedesktop.DBus.Error.UnknownProperty"
	ErrUnknownIface    = "org.freedesktop.DBus.Error.UnknownInterface"
	ErrPropertyRO      = "org.freedesktop.DBus.Error.PropertyReadOnly"
)

// Failed is fdo::Error::Failed(msg).
func Failed(msg string) *dbus.Error { return dbus.NewError(ErrFailed, []any{msg}) }

// InvalidArgs is fdo::Error::InvalidArgs(msg).
func InvalidArgs(msg string) *dbus.Error { return dbus.NewError(ErrInvalidArgs, []any{msg}) }

// Getters maps property names to their read functions. Each returns a
// value godbus can marshal; the D-Bus signature is taken from it.
type Getters map[string]func() any

// properties is org.freedesktop.DBus.Properties for the interfaces of
// one object.
type properties struct {
	ifaces map[string]Getters
}

func (p *properties) of(iface string) (Getters, *dbus.Error) {
	getters, ok := p.ifaces[iface]
	if !ok {
		return nil, dbus.NewError(ErrUnknownIface, []any{"Unknown interface '" + iface + "'"})
	}
	return getters, nil
}

// Get returns one property.
func (p *properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	getters, derr := p.of(iface)
	if derr != nil {
		return dbus.Variant{}, derr
	}
	get, ok := getters[name]
	if !ok {
		return dbus.Variant{}, dbus.NewError(ErrUnknownProperty, []any{"Unknown property '" + name + "'"})
	}
	return dbus.MakeVariant(get()), nil
}

// GetAll returns every property.
func (p *properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	getters, derr := p.of(iface)
	if derr != nil {
		return nil, derr
	}
	out := make(map[string]dbus.Variant, len(getters))
	for name, get := range getters {
		out[name] = dbus.MakeVariant(get())
	}
	return out, nil
}

// Set rejects writes: every wayle daemon property is read-only.
func (p *properties) Set(_, name string, _ dbus.Variant) *dbus.Error {
	return dbus.NewError(ErrPropertyRO, []any{"Property '" + name + "' is read-only"})
}

// Service is one exported interface.
type Service struct {
	// Name is the well-known bus name and Path the object path.
	Name string
	Path dbus.ObjectPath
	// Interface is the D-Bus interface the methods are exported as.
	Interface string
	// Methods is the object whose exported methods (each ending in a
	// *dbus.Error result) form the interface.
	Methods any
	// Properties are the read-only properties.
	Properties Getters
}

// Interface is one D-Bus interface of an object: its methods (an
// object whose exported methods each end in a *dbus.Error result) and
// its read-only properties.
type Interface struct {
	Name       string
	Methods    any
	Properties Getters
}

// Export puts ifaces at path on conn, with one Properties object serving
// all of their properties and introspection listing them all, as zbus's
// object_server().at does for each interface it mounts at a path. The
// returned unexport removes them again (a Request or Session object
// ending).
func Export(conn *dbus.Conn, path dbus.ObjectPath, ifaces ...Interface) (unexport func(), err error) {
	props := &properties{ifaces: map[string]Getters{}}
	node := &introspect.Node{Name: string(path), Interfaces: []introspect.Interface{
		introspect.IntrospectData,
		{Name: "org.freedesktop.DBus.Properties", Methods: introspect.Methods(props)},
	}}
	names := []string{"org.freedesktop.DBus.Properties", "org.freedesktop.DBus.Introspectable"}
	for _, iface := range ifaces {
		if err := conn.Export(iface.Methods, path, iface.Name); err != nil {
			return nil, fmt.Errorf("export %s: %w", iface.Name, err)
		}
		names = append(names, iface.Name)
		props.ifaces[iface.Name] = iface.Properties
		node.Interfaces = append(node.Interfaces, introspect.Interface{
			Name: iface.Name, Methods: introspect.Methods(iface.Methods), Properties: introspectProps(iface.Properties),
		})
	}
	if err := conn.Export(props, path, "org.freedesktop.DBus.Properties"); err != nil {
		return nil, fmt.Errorf("export %s properties: %w", path, err)
	}
	if err := conn.Export(introspect.NewIntrospectable(node), path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("export %s introspection: %w", path, err)
	}
	return func() {
		for _, name := range names {
			_ = conn.Export(nil, path, name)
		}
	}, nil
}

// Serve exports s on conn and takes its well-known name; the returned
// release drops the name. A name already owned is an error, as zbus's
// request_name with DoNotQueue.
func Serve(conn *dbus.Conn, s Service) (func(), error) {
	if _, err := Export(conn, s.Path, Interface{Name: s.Interface, Methods: s.Methods, Properties: s.Properties}); err != nil {
		return nil, err
	}
	return Own(conn, s.Name)
}

// Own takes a well-known name; the returned release drops it. A name
// already owned is an error, as zbus's request_name with DoNotQueue.
func Own(conn *dbus.Conn, name string) (func(), error) {
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", name, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("%s is already owned", name)
	}
	return func() { _, _ = conn.ReleaseName(name) }, nil
}

func introspectProps(getters Getters) []introspect.Property {
	names := make([]string, 0, len(getters))
	for name := range getters {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]introspect.Property, 0, len(names))
	for _, name := range names {
		out = append(out, introspect.Property{
			Name:   name,
			Type:   dbus.SignatureOf(getters[name]()).String(),
			Access: "read",
		})
	}
	return out
}
