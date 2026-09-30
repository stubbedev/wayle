package shellipc

import (
	"fmt"
	"sort"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// The shell's GApplication identity (wayle-ipc/src/shell.rs): the
// Rust shell is a GtkApplication, so the name and its org.gtk.Actions
// object come from GIO; the Go shell serves the same two actions.
const (
	AppID          = "com.wayle.shell"
	AppPath        = dbus.ObjectPath("/com/wayle/shell")
	ActionsIface   = "org.gtk.Actions"
	ActionQuit     = "quit"
	ActionInspect  = "inspector"
	errNoInspector = "the GTK inspector does not exist in the Go shell"
)

// IsRunning is bootstrap.rs's is_already_running / the CLI's
// is_running: whether a shell owns the application id.
func IsRunning(conn *dbus.Conn) (bool, error) {
	var owned bool
	err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, AppID).Store(&owned)
	return owned, err
}

// actions is GIO's action-group exporter for the two stateless
// actions.
type actions struct {
	quit func()
}

// actionDescription is org.gtk.Actions' (bgav): enabled, parameter
// type, state.
type actionDescription struct {
	Enabled   bool
	ParamType dbus.Signature
	State     []dbus.Variant
}

func (a *actions) names() []string {
	names := []string{ActionInspect, ActionQuit}
	sort.Strings(names)
	return names
}

// List names the actions.
func (a *actions) List() ([]string, *dbus.Error) { return a.names(), nil }

// Describe describes one action.
func (a *actions) Describe(name string) (actionDescription, *dbus.Error) {
	if name != ActionQuit && name != ActionInspect {
		return actionDescription{}, dbusx.InvalidArgs("action '" + name + "' does not exist")
	}
	return actionDescription{Enabled: true, State: []dbus.Variant{}}, nil
}

// DescribeAll describes every action.
func (a *actions) DescribeAll() (map[string]actionDescription, *dbus.Error) {
	out := map[string]actionDescription{}
	for _, name := range a.names() {
		out[name] = actionDescription{Enabled: true, State: []dbus.Variant{}}
	}
	return out, nil
}

// Activate runs an action.
func (a *actions) Activate(name string, _ []dbus.Variant, _ map[string]dbus.Variant) *dbus.Error {
	switch name {
	case ActionQuit:
		a.quit()
		return nil
	case ActionInspect:
		return dbusx.Failed(errNoInspector)
	default:
		return dbusx.InvalidArgs("action '" + name + "' does not exist")
	}
}

// SetState refuses: both actions are stateless.
func (a *actions) SetState(name string, _ dbus.Variant, _ map[string]dbus.Variant) *dbus.Error {
	return dbusx.InvalidArgs("action '" + name + "' is stateless")
}

// ServeApplication takes the application id and exports the actions;
// quit runs on "quit". A shell already holding the id is an error.
func ServeApplication(conn *dbus.Conn, quit func()) (func(), error) {
	a := &actions{quit: quit}
	if err := conn.Export(a, AppPath, ActionsIface); err != nil {
		return nil, fmt.Errorf("export %s: %w", ActionsIface, err)
	}
	node := &introspect.Node{
		Name: string(AppPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: ActionsIface, Methods: introspect.Methods(a)},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), AppPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("export introspection: %w", err)
	}
	reply, err := conn.RequestName(AppID, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", AppID, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("%s is already owned", AppID)
	}
	return func() { _, _ = conn.ReleaseName(AppID) }, nil
}
