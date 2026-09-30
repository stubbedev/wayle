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

// properties is org.freedesktop.DBus.Properties for one interface.
type properties struct {
	iface   string
	getters Getters
}

// Get returns one property.
func (p *properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != p.iface {
		return dbus.Variant{}, dbus.NewError(ErrUnknownIface, []any{"Unknown interface '" + iface + "'"})
	}
	get, ok := p.getters[name]
	if !ok {
		return dbus.Variant{}, dbus.NewError(ErrUnknownProperty, []any{"Unknown property '" + name + "'"})
	}
	return dbus.MakeVariant(get()), nil
}

// GetAll returns every property.
func (p *properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != p.iface {
		return nil, dbus.NewError(ErrUnknownIface, []any{"Unknown interface '" + iface + "'"})
	}
	out := make(map[string]dbus.Variant, len(p.getters))
	for name, get := range p.getters {
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

// Serve exports s on conn and takes its well-known name; the returned
// release drops the name. A name already owned is an error, as zbus's
// request_name with DoNotQueue.
func Serve(conn *dbus.Conn, s Service) (func(), error) {
	if err := conn.Export(s.Methods, s.Path, s.Interface); err != nil {
		return nil, fmt.Errorf("export %s: %w", s.Interface, err)
	}
	props := &properties{iface: s.Interface, getters: s.Properties}
	if err := conn.Export(props, s.Path, "org.freedesktop.DBus.Properties"); err != nil {
		return nil, fmt.Errorf("export %s properties: %w", s.Interface, err)
	}
	node := &introspect.Node{
		Name: string(s.Path),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: "org.freedesktop.DBus.Properties", Methods: introspect.Methods(props)},
			{Name: s.Interface, Methods: introspect.Methods(s.Methods), Properties: introspectProps(s.Properties)},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), s.Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("export %s introspection: %w", s.Interface, err)
	}
	reply, err := conn.RequestName(s.Name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", s.Name, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("%s is already owned", s.Name)
	}
	return func() { _, _ = conn.ReleaseName(s.Name) }, nil
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
