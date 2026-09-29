package sni

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// D-Bus names for the SNI protocol.
const (
	WatcherName  = "org.kde.StatusNotifierWatcher"
	WatcherPath  = "/StatusNotifierWatcher"
	WatcherIface = "org.kde.StatusNotifierWatcher"
	HostIface    = "org.kde.StatusNotifierHost"
	ItemIface    = "org.kde.StatusNotifierItem"
	PropsIface   = "org.freedesktop.DBus.Properties"
)

// Host plays the host role against a watcher (ours or an existing
// one) and tracks items.
type Host struct {
	conn  *dbus.Conn
	store *Store

	mu      sync.Mutex
	owned   bool
	watcher bool // true: we serve the watcher role ourselves
	release func()
	stop    chan struct{}
}

// NewHost connects to the session bus and claims the roles: when no
// watcher is running, this process becomes the watcher and hosts
// against itself.
func NewHost(store *Store) (*Host, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("sni: session bus: %w", err)
	}
	h := &Host{conn: conn, store: store, stop: make(chan struct{})}
	watchOwned, err := h.claimWatcher()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	h.watcher = watchOwned
	if err := h.registerHost(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := h.subscribe(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go h.run()
	return h, nil
}

// claimWatcher exports the watcher object and requests its name when
// nobody owns it; false return means a watcher already runs.
func (h *Host) claimWatcher() (bool, error) {
	obj := h.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var owner string
	err := obj.Call("org.freedesktop.DBus.GetNameOwner", 0, WatcherName).Store(&owner)
	if err == nil && owner != "" {
		return false, nil
	}
	w := &watcher{store: h.store}
	if err := h.conn.Export(w, WatcherPath, WatcherIface); err != nil {
		return false, fmt.Errorf("sni: export watcher: %w", err)
	}
	reply, err := h.conn.RequestName(WatcherName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return false, fmt.Errorf("sni: request watcher name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return false, nil
	}
	h.owned = true
	return true, nil
}

// registerHost announces the host to the watcher.
func (h *Host) registerHost() error {
	return h.conn.Object(WatcherName, WatcherPath).Call(WatcherIface+".RegisterStatusNotifierHost", 0, uniqueName(h.conn)).Err
}

// uniqueName reads the connection's own bus name.
func uniqueName(conn *dbus.Conn) string {
	for _, name := range conn.Names() {
		if strings.HasPrefix(name, ":") {
			return name
		}
	}
	return ""
}

// subscribe matches the watcher's item-registration signals.
func (h *Host) subscribe() error {
	for _, sig := range []string{"StatusNotifierItemRegistered", "StatusNotifierItemUnregistered", "StatusNotifierHostRegistered"} {
		if err := h.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(WatcherPath),
			dbus.WithMatchInterface(WatcherIface),
			dbus.WithMatchMember(sig),
		); err != nil {
			return fmt.Errorf("sni: match %s: %w", sig, err)
		}
	}
	return nil
}

// run consumes the watcher's signals: registrations read a fresh item
// snapshot, unregistrations drop it. A full resync also picks up items
// that registered before we connected.
func (h *Host) run() {
	c := make(chan *dbus.Signal, 16)
	h.conn.Signal(c)
	defer h.conn.RemoveSignal(c)
	h.resync()
	for {
		select {
		case <-h.stop:
			return
		case sig := <-c:
			switch sig.Name {
			case WatcherIface + ".StatusNotifierItemRegistered":
				if len(sig.Body) == 1 {
					if reg, ok := sig.Body[0].(string); ok {
						h.track(reg)
					}
				}
			case WatcherIface + ".StatusNotifierItemUnregistered":
				if len(sig.Body) == 1 {
					if reg, ok := sig.Body[0].(string); ok {
						bus, path := ParseAddress(reg)
						h.store.Remove(bus, path)
					}
				}
			case WatcherIface + ".StatusNotifierHostRegistered":
				// A peer host joined; nothing to track.
			}
		}
	}
}

// resync reads the watcher's RegisteredStatusNotifierItems.
func (h *Host) resync() {
	obj := h.conn.Object(WatcherName, WatcherPath)
	var regs []string
	if err := obj.Call(PropsIface+".Get", 0, WatcherIface, "RegisteredStatusNotifierItems").Store(&regs); err != nil {
		return
	}
	for _, reg := range regs {
		h.track(reg)
	}
}

// track reads one item's properties into the store.
func (h *Host) track(reg string) {
	bus, path := ParseAddress(reg)
	item, err := readItem(h.conn, bus, path)
	if err != nil {
		log.Printf("sni: %s: %v", reg, err)
		return
	}
	h.store.Put(item)
}

// readItem fetches the SNI property set.
func readItem(conn *dbus.Conn, bus, path string) (*Item, error) {
	obj := conn.Object(bus, dbus.ObjectPath(path))
	var props map[string]dbus.Variant
	if err := obj.Call(PropsIface+".GetAll", 0, ItemIface).Store(&props); err != nil {
		return nil, fmt.Errorf("GetAll: %w", err)
	}
	it := &Item{
		Bus:  bus,
		Path: path,
	}
	str := func(name string) string {
		if v, ok := props[name]; ok {
			s, _ := v.Value().(string)
			return s
		}
		return ""
	}
	boolean := func(name string) bool {
		if v, ok := props[name]; ok {
			b, _ := v.Value().(bool)
			return b
		}
		return false
	}
	it.ID = str("Id")
	it.Title = str("Title")
	it.Category = ParseCategory(str("Category"))
	it.Status = ParseStatus(str("Status"))
	it.ItemIsMenu = boolean("ItemIsMenu")
	it.IconName = str("IconName")
	it.OverlayIconName = str("OverlayIconName")
	it.AttentionName = str("AttentionIconName")
	it.AttentionMovie = str("AttentionMovieName")
	it.IconThemePath = str("IconThemePath")
	it.MenuPath = str("Menu")
	if pixmaps, ok := props["IconPixmap"]; ok {
		it.IconPixmap = parsePixmaps(pixmaps.Value())
	}
	if tooltip, ok := props["ToolTip"]; ok {
		it.Tooltip = parseTooltip(tooltip.Value())
	}
	return it, nil
}

// parsePixmaps converts the spec's (width, height, data) list.
func parsePixmaps(v any) []Pixmap {
	rows, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []Pixmap
	for _, row := range rows {
		tuple, ok := row.([]any)
		if len(tuple) != 3 || !ok {
			continue
		}
		w, _ := tuple[0].(int32)
		hgt, _ := tuple[1].(int32)
		data, _ := tuple[2].([]byte)
		out = append(out, Pixmap{Width: w, Height: hgt, Data: data})
	}
	return out
}

// parseTooltip converts the spec's (icon, pixmaps, title, desc).
func parseTooltip(v any) Tooltip {
	tuple, ok := v.([]any)
	if len(tuple) != 4 || !ok {
		return Tooltip{}
	}
	tip := Tooltip{}
	tip.IconName, _ = tuple[0].(string)
	tip.IconPixmap = parsePixmaps(tuple[1])
	tip.Title, _ = tuple[2].(string)
	tip.Description, _ = tuple[3].(string)
	return tip
}

// Close releases the watcher name when we own it and stops the loop.
func (h *Host) Close() error {
	close(h.stop)
	if h.owned {
		_, _ = h.conn.ReleaseName(WatcherName)
	}
	return h.conn.Close()
}

// watcher is the exported StatusNotifierWatcher object.
type watcher struct {
	store *Store
}

// RegisterStatusNotifierItem accepts an item's registration.
func (w *watcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	_ = service
	return nil
}

// RegisterStatusNotifierHost accepts a host's registration.
func (w *watcher) RegisterStatusNotifierHost(service string) *dbus.Error {
	_ = service
	return nil
}

// IsStatusNotifierHostRegistered is always true while we watch.
func (w *watcher) IsStatusNotifierHostRegistered() (bool, error) { return true, nil }

// ProtocolVersion is the SNI protocol version.
func (w *watcher) ProtocolVersion() (int32, error) { return 0, nil }

// RegisteredStatusNotifierItems lists the tracked items.
func (w *watcher) RegisteredStatusNotifierItems() ([]string, error) {
	items := w.store.Items()
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Bus+it.Path)
	}
	return out, nil
}

// Actions drives one item (the bar's click handlers).
type Actions struct {
	conn *dbus.Conn
}

// Activate sends the primary activation at the given coordinates.
func (a *Actions) Activate(ctx context.Context, it Item, x, y int32) error {
	obj := a.conn.Object(it.Bus, dbus.ObjectPath(it.Path))
	return obj.CallWithContext(ctx, ItemIface+".Activate", 0, x, y).Err
}

// SecondaryActivate sends the middle-click activation.
func (a *Actions) SecondaryActivate(ctx context.Context, it Item, x, y int32) error {
	obj := a.conn.Object(it.Bus, dbus.ObjectPath(it.Path))
	return obj.CallWithContext(ctx, ItemIface+".SecondaryActivate", 0, x, y).Err
}

// ContextMenu asks the item to show its own menu.
func (a *Actions) ContextMenu(ctx context.Context, it Item, x, y int32) error {
	obj := a.conn.Object(it.Bus, dbus.ObjectPath(it.Path))
	return obj.CallWithContext(ctx, ItemIface+".ContextMenu", 0, x, y).Err
}

// Scroll sends a scroll delta ("horizontal"/"vertical").
func (a *Actions) Scroll(ctx context.Context, it Item, delta int32, orientation string) error {
	obj := a.conn.Object(it.Bus, dbus.ObjectPath(it.Path))
	return obj.CallWithContext(ctx, ItemIface+".Scroll", 0, delta, orientation).Err
}
