package portal

import (
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// GlobalShortcutsIface bridges org.freedesktop.impl.portal.GlobalShortcuts
// to the compositor's hyprland-global-shortcuts-v1, as
// xdg-desktop-portal-hyprland does (globalshortcuts/mod.rs): bound
// shortcuts are registered there and their presses come back as
// Activated / Deactivated. Compositors without the protocol (niri,
// mango) accept binds and never activate them.
const GlobalShortcutsIface = "org.freedesktop.impl.portal.GlobalShortcuts"

// shortcutDevice is the compositor side (waylandShortcuts; a fake in
// tests).
type shortcutDevice interface {
	// register binds id for appID under key, the route its events carry;
	// the returned func unregisters it.
	register(key, id, appID, description, trigger string) (unregister func(), err error)
}

// shortcut is one (sa{sv}): the id and the properties echoed back.
type shortcut struct {
	ID    string
	Props Vardict
}

// shortcutRoute is where a registration's events go.
type shortcutRoute struct {
	session dbus.ObjectPath
	id      string
}

type shortcutSession struct {
	appID     string
	shortcuts []shortcut
}

// globalShortcuts is the interface's state.
type globalShortcuts struct {
	conn     *dbus.Conn
	sessions *sessions
	// start brings the device up on first use; onEvent gets every
	// press and release.
	start func(onEvent func(key string, pressed bool, sec uint64)) (shortcutDevice, error)

	mu      sync.Mutex
	dev     shortcutDevice
	data    map[dbus.ObjectPath]*shortcutSession
	routes  map[string]shortcutRoute
	objects map[string]func()
}

func newGlobalShortcuts(conn *dbus.Conn, s *sessions, start func(func(string, bool, uint64)) (shortcutDevice, error)) *globalShortcuts {
	return &globalShortcuts{
		conn: conn, sessions: s, start: start,
		data: map[dbus.ObjectPath]*shortcutSession{}, routes: map[string]shortcutRoute{}, objects: map[string]func(){},
	}
}

func (g *globalShortcuts) iface() dbusx.Interface {
	return dbusx.Interface{
		Name:       GlobalShortcutsIface,
		Methods:    globalShortcutsObject{g},
		Properties: dbusx.Getters{"version": func() any { return uint32(1) }},
	}
}

// shortcutKey ties a compositor registration to its route; the unit
// separator keeps ("ab", "") apart from ("a", "b").
func shortcutKey(appID, id string) string { return appID + "\x1f" + id }

// device starts the device once; nil without one.
func (g *globalShortcuts) device() shortcutDevice {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dev != nil {
		return g.dev
	}
	dev, err := g.start(g.event)
	if err != nil {
		warnf("global shortcuts unavailable on this compositor: %v", err)
		return nil
	}
	g.dev = dev
	return dev
}

func (g *globalShortcuts) event(key string, pressed bool, sec uint64) {
	g.mu.Lock()
	route, ok := g.routes[key]
	g.mu.Unlock()
	if !ok {
		return
	}
	member := ".Deactivated"
	if pressed {
		member = ".Activated"
	}
	_ = g.conn.Emit(ObjectPath, GlobalShortcutsIface+member, route.session, route.id, sec, Vardict{})
}

// register binds the shortcuts not yet registered for appID; a re-bind
// of a registered app_id + id reuses it, since a second registration
// of the pair is the compositor's already_taken error.
func (g *globalShortcuts) register(session dbus.ObjectPath, appID string, shortcuts []shortcut) {
	if len(shortcuts) == 0 {
		return
	}
	dev := g.device()
	if dev == nil {
		return
	}
	for _, s := range shortcuts {
		key := shortcutKey(appID, s.ID)
		g.mu.Lock()
		_, taken := g.objects[key]
		g.mu.Unlock()
		if taken {
			continue
		}
		unregister, err := dev.register(key, s.ID, appID, stringOr(s.Props, "description", ""), stringOr(s.Props, "preferred_trigger", ""))
		if err != nil {
			warnf("global shortcuts: register %s: %v", s.ID, err)
			continue
		}
		g.mu.Lock()
		g.routes[key] = shortcutRoute{session, s.ID}
		g.objects[key] = unregister
		g.mu.Unlock()
	}
}

// closeSession forgets the session's shortcuts and unregisters them.
func (g *globalShortcuts) closeSession(session dbus.ObjectPath) {
	g.mu.Lock()
	delete(g.data, session)
	var stale []func()
	for key, r := range g.routes {
		if r.session == session {
			delete(g.routes, key)
			if unregister, ok := g.objects[key]; ok {
				stale = append(stale, unregister)
				delete(g.objects, key)
			}
		}
	}
	g.mu.Unlock()
	for _, unregister := range stale {
		unregister()
	}
}

// shortcutsOf is the session's bound shortcuts, none for an unknown
// session.
func (g *globalShortcuts) shortcutsOf(session dbus.ObjectPath) []shortcut {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d, ok := g.data[session]; ok {
		return append([]shortcut{}, d.shortcuts...)
	}
	return []shortcut{}
}

// mergeShortcuts replaces the props of ids already present, in place,
// and appends the new ones, as xdph treats a re-bind as an update.
func mergeShortcuts(existing, incoming []shortcut) []shortcut {
	for _, in := range incoming {
		found := false
		for i := range existing {
			if existing[i].ID == in.ID {
				existing[i].Props = in.Props
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, in)
		}
	}
	return existing
}

// globalShortcutsObject carries the interface's D-Bus methods.
type globalShortcutsObject struct{ g *globalShortcuts }

// CreateSession opens a session and registers the shortcuts its
// options carry (xdph does too).
func (o globalShortcutsObject) CreateSession(_, session dbus.ObjectPath, appID string, options Vardict) (uint32, Vardict, *dbus.Error) {
	if err := o.g.sessions.mount(session, func() { o.g.closeSession(session) }); err != nil {
		warnf("global shortcuts: cannot mount session: %v", err)
		return ResponseOther, Vardict{}, nil
	}
	var shortcuts []shortcut
	if v, ok := options["shortcuts"]; ok {
		if err := dbus.Store([]any{v.Value()}, &shortcuts); err != nil {
			shortcuts = nil
		}
	}
	o.g.mu.Lock()
	o.g.data[session] = &shortcutSession{appID: appID, shortcuts: shortcuts}
	o.g.mu.Unlock()
	o.g.register(session, appID, shortcuts)
	return ResponseSuccess, Vardict{}, nil
}

// BindShortcuts registers shortcuts and replies the session's merged
// set.
func (o globalShortcutsObject) BindShortcuts(_, session dbus.ObjectPath, shortcuts []shortcut, _ string, _ Vardict) (uint32, Vardict, *dbus.Error) {
	o.g.mu.Lock()
	appID := ""
	if d, ok := o.g.data[session]; ok {
		appID = d.appID
	}
	o.g.mu.Unlock()
	o.g.register(session, appID, shortcuts)
	o.g.mu.Lock()
	if d, ok := o.g.data[session]; ok {
		d.shortcuts = mergeShortcuts(d.shortcuts, shortcuts)
	}
	o.g.mu.Unlock()
	return ResponseSuccess, Vardict{"shortcuts": dbus.MakeVariant(o.g.shortcutsOf(session))}, nil
}

// ListShortcuts replies the session's bound shortcuts.
func (o globalShortcutsObject) ListShortcuts(_, session dbus.ObjectPath) (uint32, Vardict, *dbus.Error) {
	return ResponseSuccess, Vardict{"shortcuts": dbus.MakeVariant(o.g.shortcutsOf(session))}, nil
}
