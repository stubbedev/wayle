package sni

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

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
	// ProtocolVersion is the watcher's advertised protocol version.
	ProtocolVersion = int32(0)
)

// Discovery cadences (watcher/discovery.rs, core/item/monitoring.rs).
var (
	// orphanScanSchedule re-scans the bus for items that registered with
	// a previous watcher (a shell restart) and never re-register.
	orphanScanSchedule = []time.Duration{0, time.Second, 3 * time.Second, 8 * time.Second, 20 * time.Second}
	probeTimeout       = 500 * time.Millisecond
	// menuDebounce coalesces a burst of DBusMenu change signals into
	// one refetch.
	menuDebounce = 100 * time.Millisecond
)

// Host plays the host role against a watcher (ours or an existing
// one), tracks items and their property changes, and hands out the
// item controls.
type Host struct {
	conn  *dbus.Conn
	store *Store

	owned   bool
	watcher *watcher // non-nil: we serve the watcher role ourselves
	stop    chan struct{}

	mu sync.Mutex
	// owners maps each item key to its connection's unique name, so a
	// signal's sender resolves to the items it belongs to.
	owners map[string]string
	// menus are the open menus' change feeds, by item key.
	menus map[string][]chan struct{}
}

// NewHost connects to the session bus and claims the roles: when no
// watcher is running, this process becomes the watcher and hosts
// against itself.
func NewHost(store *Store) (*Host, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("sni: session bus: %w", err)
	}
	h, err := NewHostOn(conn, store)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return h, nil
}

// NewHostOn runs the host on an existing connection (tests use a
// private bus).
func NewHostOn(conn *dbus.Conn, store *Store) (*Host, error) {
	h := &Host{
		conn:   conn,
		store:  store,
		stop:   make(chan struct{}),
		owners: make(map[string]string),
		menus:  make(map[string][]chan struct{}),
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	if err := h.subscribe(); err != nil {
		return nil, err
	}
	if err := h.claimWatcher(); err != nil {
		return nil, err
	}
	if h.watcher == nil {
		if err := h.registerHost(); err != nil {
			return nil, err
		}
	}
	go h.run(signals)
	if h.watcher == nil {
		go h.resync()
	} else {
		go h.orphanScan()
	}
	return h, nil
}

// Actions returns the item controls on the host's connection.
func (h *Host) Actions() *Actions { return &Actions{conn: h.conn} }

// claimWatcher exports the watcher object and requests its name when
// nobody owns it; h.watcher stays nil when a watcher already runs.
func (h *Host) claimWatcher() error {
	var owner string
	err := h.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, WatcherName).Store(&owner)
	if err == nil && owner != "" {
		return nil
	}
	w := &watcher{host: h, hosts: []string{h.conn.Names()[0]}}
	if err := h.conn.Export(w, WatcherPath, WatcherIface); err != nil {
		return fmt.Errorf("sni: export watcher: %w", err)
	}
	if err := h.conn.Export(w, WatcherPath, PropsIface); err != nil {
		return fmt.Errorf("sni: export watcher properties: %w", err)
	}
	reply, err := h.conn.RequestName(WatcherName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("sni: request watcher name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil
	}
	h.owned = true
	h.watcher = w
	return nil
}

// registerHost announces the host to an external watcher.
func (h *Host) registerHost() error {
	return h.conn.Object(WatcherName, WatcherPath).Call(WatcherIface+".RegisterStatusNotifierHost", 0, h.conn.Names()[0]).Err
}

// subscribe matches the watcher's registration signals, every item's
// change signals and PropertiesChanged, the menus' change signals, and
// bus-name ownership changes (for the watcher role and item exits).
func (h *Host) subscribe() error {
	for _, rule := range [][]dbus.MatchOption{
		{dbus.WithMatchInterface(WatcherIface)},
		{dbus.WithMatchInterface(ItemIface)},
		{dbus.WithMatchInterface(PropsIface), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchArg(0, ItemIface)},
		{dbus.WithMatchInterface(MenuIface)},
		{dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged")},
	} {
		if err := h.conn.AddMatchSignal(rule...); err != nil {
			return fmt.Errorf("sni: match: %w", err)
		}
	}
	return nil
}

// run consumes the signals until Close.
func (h *Host) run(signals chan *dbus.Signal) {
	defer h.conn.RemoveSignal(signals)
	for {
		select {
		case <-h.stop:
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			h.handle(sig)
		}
	}
}

// handle routes one signal.
func (h *Host) handle(sig *dbus.Signal) {
	member := sig.Name[strings.LastIndex(sig.Name, ".")+1:]
	iface := strings.TrimSuffix(sig.Name, "."+member)
	switch {
	case iface == WatcherIface && h.watcher == nil:
		// An external watcher's feed; our own watcher tracks directly.
		reg, _ := firstString(sig.Body)
		switch member {
		case "StatusNotifierItemRegistered":
			h.track(reg)
		case "StatusNotifierItemUnregistered":
			bus, path := ParseAddress(reg)
			h.forget(bus, path)
		}
	case iface == ItemIface, iface == PropsIface:
		// NewIcon, NewTitle, NewStatus, NewToolTip, NewMenu, ... and
		// PropertiesChanged: re-read the item's snapshot.
		for _, key := range h.itemsOf(sig.Sender, string(sig.Path)) {
			if it, ok := h.store.Get(key); ok {
				h.track(it.Bus + it.Path)
			}
		}
	case iface == MenuIface:
		// LayoutUpdated, ItemsPropertiesUpdated: open menus refetch.
		for _, key := range h.menuItemsOf(sig.Sender, string(sig.Path)) {
			h.notifyMenu(key)
		}
	case sig.Name == "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) != 3 {
			return
		}
		name, _ := sig.Body[0].(string)
		newOwner, _ := sig.Body[2].(string)
		if newOwner != "" {
			return
		}
		if h.watcher != nil {
			h.watcher.nameVanished(name)
			return
		}
		h.forgetName(name)
	}
}

// firstString is a one-string signal body.
func firstString(body []any) (string, bool) {
	if len(body) != 1 {
		return "", false
	}
	s, ok := body[0].(string)
	return s, ok
}

// itemsOf lists the tracked items a signal from sender at path is
// about.
func (h *Host) itemsOf(sender, path string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var keys []string
	for key, owner := range h.owners {
		if owner != sender {
			continue
		}
		if it, ok := h.store.Get(key); ok && it.Path == path {
			keys = append(keys, key)
		}
	}
	return keys
}

// menuItemsOf lists the tracked items whose menu object a signal came
// from.
func (h *Host) menuItemsOf(sender, path string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var keys []string
	for key, owner := range h.owners {
		if owner != sender {
			continue
		}
		if it, ok := h.store.Get(key); ok && it.MenuPath == path {
			keys = append(keys, key)
		}
	}
	return keys
}

// resync reads an external watcher's RegisteredStatusNotifierItems.
func (h *Host) resync() {
	var regs []string
	if err := h.conn.Object(WatcherName, WatcherPath).Call(PropsIface+".Get", 0, WatcherIface, "RegisteredStatusNotifierItems").Store(&regs); err != nil {
		return
	}
	for _, reg := range regs {
		h.track(reg)
	}
}

// orphanScan is discovery.rs spawn_orphan_scan: on the schedule,
// probe every other unique name for a StatusNotifierItem and register
// the ones that answer - items that registered with a watcher that has
// since gone away do not re-register on their own.
func (h *Host) orphanScan() {
	for _, delay := range orphanScanSchedule {
		select {
		case <-h.stop:
			return
		case <-time.After(delay):
		}
		var names []string
		if err := h.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
			return
		}
		own := h.conn.Names()[0]
		for _, name := range names {
			if !strings.HasPrefix(name, ":") || name == own || h.watcher.registered(name) {
				continue
			}
			if h.probe(name) {
				h.watcher.register(name)
			}
		}
	}
}

// probe reports whether name serves a StatusNotifierItem at the
// default path (its Id reads within the probe timeout).
func (h *Host) probe(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var v dbus.Variant
	return h.conn.Object(name, "/StatusNotifierItem").CallWithContext(ctx, PropsIface+".Get", 0, ItemIface, "Id").Store(&v) == nil
}

// track reads one item's properties into the store and remembers its
// owner.
func (h *Host) track(reg string) {
	bus, path := ParseAddress(reg)
	item, err := readItem(h.conn, bus, path)
	if err != nil {
		log.Printf("sni: %s: %v", reg, err)
		return
	}
	owner := bus
	if !strings.HasPrefix(bus, ":") {
		var unique string
		if err := h.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, bus).Store(&unique); err == nil {
			owner = unique
		}
	}
	h.mu.Lock()
	h.owners[item.Key()] = owner
	h.mu.Unlock()
	h.store.Put(item)
}

// forget drops one item.
func (h *Host) forget(bus, path string) {
	h.mu.Lock()
	delete(h.owners, bus+path)
	h.mu.Unlock()
	h.store.Remove(bus, path)
}

// forgetName drops every item a vanished connection owned (an external
// watcher normally unregisters them, but a crashed item may not be).
func (h *Host) forgetName(name string) {
	for _, it := range h.store.Items() {
		h.mu.Lock()
		owner := h.owners[it.Key()]
		h.mu.Unlock()
		if owner == name || it.Bus == name {
			h.forget(it.Bus, it.Path)
		}
	}
}

// WatchMenu returns a feed that ticks (debounced) whenever the item's
// menu layout or properties change, for as long as a menu is open;
// stop releases it.
func (h *Host) WatchMenu(key string) (<-chan struct{}, func()) {
	raw := make(chan struct{}, 1)
	out := make(chan struct{}, 1)
	h.mu.Lock()
	h.menus[key] = append(h.menus[key], raw)
	h.mu.Unlock()
	done := make(chan struct{})
	go func() {
		var timer <-chan time.Time
		for {
			select {
			case <-done:
				return
			case <-raw:
				timer = time.After(menuDebounce)
			case <-timer:
				timer = nil
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	stop := func() {
		h.mu.Lock()
		feeds := h.menus[key]
		for i, ch := range feeds {
			if ch == raw {
				h.menus[key] = append(feeds[:i], feeds[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
		close(done)
	}
	return out, stop
}

// notifyMenu ticks an item's open menu feeds.
func (h *Host) notifyMenu(key string) {
	h.mu.Lock()
	feeds := append([]chan struct{}(nil), h.menus[key]...)
	h.mu.Unlock()
	for _, ch := range feeds {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// readItem fetches the SNI property set.
func readItem(conn *dbus.Conn, bus, path string) (*Item, error) {
	obj := conn.Object(bus, dbus.ObjectPath(path))
	var props map[string]dbus.Variant
	if err := obj.Call(PropsIface+".GetAll", 0, ItemIface).Store(&props); err != nil {
		return nil, fmt.Errorf("GetAll: %w", err)
	}
	it := &Item{Bus: bus, Path: path}
	str := func(name string) string {
		if v, ok := props[name]; ok {
			switch s := v.Value().(type) {
			case string:
				return s
			case dbus.ObjectPath:
				return string(s)
			}
		}
		return ""
	}
	it.ID = str("Id")
	it.Title = str("Title")
	it.Category = ParseCategory(str("Category"))
	it.Status = ParseStatus(str("Status"))
	if v, ok := props["ItemIsMenu"]; ok {
		it.ItemIsMenu, _ = v.Value().(bool)
	}
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

// parsePixmaps converts the spec's a(iiay) list.
func parsePixmaps(v any) []Pixmap {
	rows, ok := v.([][]any)
	if !ok {
		if anyRows, ok := v.([]any); ok {
			for _, r := range anyRows {
				if tuple, ok := r.([]any); ok {
					rows = append(rows, tuple)
				}
			}
		}
	}
	var out []Pixmap
	for _, tuple := range rows {
		if len(tuple) != 3 {
			continue
		}
		w, _ := tuple[0].(int32)
		hgt, _ := tuple[1].(int32)
		data, _ := tuple[2].([]byte)
		out = append(out, Pixmap{Width: w, Height: hgt, Data: data})
	}
	return out
}

// parseTooltip converts the spec's (sa(iiay)ss).
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

// watcher is the exported StatusNotifierWatcher (watcher/mod.rs).
type watcher struct {
	host *Host

	mu    sync.Mutex
	items []string
	hosts []string
}

// RegisterStatusNotifierItem accepts an item. A bare object path is
// the sender's own (sender + path), as the spec and the Rust watcher
// allow; anything else is a bus name.
func (w *watcher) RegisterStatusNotifierItem(sender dbus.Sender, service string) *dbus.Error {
	if strings.HasPrefix(service, "/") {
		service = string(sender) + service
	}
	w.register(service)
	return nil
}

// register records an item, announces it, and tracks it. A bus name
// alone and that name at the default path are one item: the orphan
// scan registers the bare name while the item itself may register
// name/StatusNotifierItem, and either order must leave one entry.
func (w *watcher) register(service string) {
	bus, path := ParseAddress(service)
	w.mu.Lock()
	if slices.ContainsFunc(w.items, func(s string) bool {
		b, p := ParseAddress(s)
		return b == bus && p == path
	}) {
		w.mu.Unlock()
		return
	}
	w.items = append(w.items, service)
	w.mu.Unlock()
	_ = w.host.conn.Emit(WatcherPath, WatcherIface+".StatusNotifierItemRegistered", service)
	w.host.track(service)
}

// registered reports whether a bus name has any item registered.
func (w *watcher) registered(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.items {
		if s == name || strings.HasPrefix(s, name+"/") {
			return true
		}
	}
	return false
}

// nameVanished is unregister_item + unregister_host for a connection
// that left the bus.
func (w *watcher) nameVanished(name string) {
	w.mu.Lock()
	var removed []string
	kept := w.items[:0]
	for _, s := range w.items {
		if s == name || strings.HasPrefix(s, name+"/") {
			removed = append(removed, s)
			continue
		}
		kept = append(kept, s)
	}
	w.items = kept
	hostGone, emptied := false, false
	for i, h := range w.hosts {
		if h == name {
			w.hosts = append(w.hosts[:i], w.hosts[i+1:]...)
			hostGone, emptied = true, len(w.hosts) == 0
			break
		}
	}
	w.mu.Unlock()
	for _, s := range removed {
		_ = w.host.conn.Emit(WatcherPath, WatcherIface+".StatusNotifierItemUnregistered", s)
		bus, path := ParseAddress(s)
		w.host.forget(bus, path)
	}
	if hostGone && emptied {
		_ = w.host.conn.Emit(WatcherPath, WatcherIface+".StatusNotifierHostUnregistered")
	}
}

// RegisterStatusNotifierHost accepts a host; the first one beyond our
// own announces itself.
func (w *watcher) RegisterStatusNotifierHost(service string) *dbus.Error {
	w.mu.Lock()
	if slices.Contains(w.hosts, service) {
		w.mu.Unlock()
		return nil
	}
	w.hosts = append(w.hosts, service)
	w.mu.Unlock()
	_ = w.host.conn.Emit(WatcherPath, WatcherIface+".StatusNotifierHostRegistered")
	return nil
}

// Get implements the Properties read of the watcher's three
// properties.
func (w *watcher) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	if iface != WatcherIface {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("unknown interface %s", iface))
	}
	all, _ := w.GetAll(iface)
	v, ok := all[prop]
	if !ok {
		return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownProperty", Body: []any{prop}}
	}
	return v, nil
}

// GetAll implements the Properties read of the watcher.
func (w *watcher) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != WatcherIface {
		return nil, dbus.MakeFailedError(fmt.Errorf("unknown interface %s", iface))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return map[string]dbus.Variant{
		"RegisteredStatusNotifierItems":  dbus.MakeVariant(append([]string{}, w.items...)),
		"IsStatusNotifierHostRegistered": dbus.MakeVariant(len(w.hosts) > 0),
		"ProtocolVersion":                dbus.MakeVariant(ProtocolVersion),
	}, nil
}

// Set rejects writes: the watcher's properties are read-only.
func (w *watcher) Set(string, string, dbus.Variant) *dbus.Error {
	return &dbus.Error{Name: "org.freedesktop.DBus.Error.PropertyReadOnly"}
}

// Actions drives one item (the bar's click handlers).
type Actions struct {
	conn *dbus.Conn
}

// Activate sends the primary activation at the given coordinates.
func (a *Actions) Activate(ctx context.Context, it Item, x, y int32) error {
	return a.conn.Object(it.Bus, dbus.ObjectPath(it.Path)).CallWithContext(ctx, ItemIface+".Activate", 0, x, y).Err
}

// SecondaryActivate sends the middle-click activation.
func (a *Actions) SecondaryActivate(ctx context.Context, it Item, x, y int32) error {
	return a.conn.Object(it.Bus, dbus.ObjectPath(it.Path)).CallWithContext(ctx, ItemIface+".SecondaryActivate", 0, x, y).Err
}

// ContextMenu asks the item to show its own menu.
func (a *Actions) ContextMenu(ctx context.Context, it Item, x, y int32) error {
	return a.conn.Object(it.Bus, dbus.ObjectPath(it.Path)).CallWithContext(ctx, ItemIface+".ContextMenu", 0, x, y).Err
}

// Scroll sends a scroll delta ("horizontal"/"vertical").
func (a *Actions) Scroll(ctx context.Context, it Item, delta int32, orientation string) error {
	return a.conn.Object(it.Bus, dbus.ObjectPath(it.Path)).CallWithContext(ctx, ItemIface+".Scroll", 0, delta, orientation).Err
}

// IsWatcher reports whether this process serves the watcher role (the
// Rust service in TrayMode::Auto having claimed it).
func (h *Host) IsWatcher() bool { return h.watcher != nil }
