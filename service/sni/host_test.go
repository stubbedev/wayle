package sni

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeMenu is a DBusMenu with one check row, a submenu, and a
// separator; it records AboutToShow and Event calls.
type fakeMenu struct {
	mu      sync.Mutex
	shown   []int32
	clicked []int32
}

func node(id int32, props map[string]dbus.Variant, children ...dbus.Variant) dbus.Variant {
	if children == nil {
		children = []dbus.Variant{}
	}
	return dbus.MakeVariant(struct {
		ID       int32
		Props    map[string]dbus.Variant
		Children []dbus.Variant
	}{id, props, children})
}

func (m *fakeMenu) GetLayout(parent, depth int32, props []string) (uint32, struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}, *dbus.Error,
) {
	root := struct {
		ID       int32
		Props    map[string]dbus.Variant
		Children []dbus.Variant
	}{0, map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, []dbus.Variant{
		node(1, map[string]dbus.Variant{"label": dbus.MakeVariant("_Mute"), "toggle-type": dbus.MakeVariant("checkmark"), "toggle-state": dbus.MakeVariant(int32(1))}),
		node(2, map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}),
		node(3, map[string]dbus.Variant{"label": dbus.MakeVariant("More"), "children-display": dbus.MakeVariant("submenu")},
			node(4, map[string]dbus.Variant{"label": dbus.MakeVariant("Quit"), "shortcut": dbus.MakeVariant([][]string{{"Control", "q"}}), "enabled": dbus.MakeVariant(false)})),
		node(5, map[string]dbus.Variant{"label": dbus.MakeVariant("Hidden"), "visible": dbus.MakeVariant(false)}),
	}}
	return 7, root, nil
}

func (m *fakeMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	m.mu.Lock()
	m.shown = append(m.shown, id)
	m.mu.Unlock()
	return false, nil
}

func (m *fakeMenu) Event(id int32, event string, _ dbus.Variant, _ uint32) *dbus.Error {
	if event == "clicked" {
		m.mu.Lock()
		m.clicked = append(m.clicked, id)
		m.mu.Unlock()
	}
	return nil
}

// fakeItem is one tray item: the SNI properties plus its menu.
type fakeItem struct {
	conn  *dbus.Conn
	props *prop.Properties
	menu  *fakeMenu
}

func startItem(t *testing.T, bus *dbustest.Bus, id string) *fakeItem {
	t.Helper()
	conn := bus.Conn(t)
	it := &fakeItem{conn: conn, menu: &fakeMenu{}}
	props, err := prop.Export(conn, "/StatusNotifierItem", prop.Map{ItemIface: {
		"Id":         {Value: id, Emit: prop.EmitConst},
		"Title":      {Value: id + " title", Emit: prop.EmitTrue},
		"Status":     {Value: "Active", Emit: prop.EmitTrue},
		"IconName":   {Value: "first-icon", Emit: prop.EmitFalse},
		"ItemIsMenu": {Value: false, Emit: prop.EmitConst},
		"Menu":       {Value: dbus.ObjectPath("/MenuBar"), Emit: prop.EmitConst},
		"IconPixmap": {Value: []struct {
			W, H int32
			Data []byte
		}{{1, 1, []byte{0xff, 0xff, 0, 0}}}, Emit: prop.EmitFalse},
	}})
	if err != nil {
		t.Fatal(err)
	}
	it.props = props
	if err := conn.Export(it.menu, "/MenuBar", MenuIface); err != nil {
		t.Fatal(err)
	}
	return it
}

func (it *fakeItem) register(t *testing.T) {
	t.Helper()
	if err := it.conn.Object(WatcherName, WatcherPath).Call(WatcherIface+".RegisterStatusNotifierItem", 0, "/StatusNotifierItem").Err; err != nil {
		t.Fatalf("register: %v", err)
	}
}

func waitItems(t *testing.T, s *Store, cond func([]Item) bool) []Item {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		items := s.Items()
		if cond(items) {
			return items
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out; items = %+v", items)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHostAsWatcherTracksItemsAndMenus(t *testing.T) {
	bus := dbustest.Start(t)
	store := NewStore()
	host, err := NewHostOn(bus.Conn(t), store)
	if err != nil {
		t.Fatalf("NewHostOn: %v", err)
	}
	defer host.Close()
	if host.watcher == nil {
		t.Fatal("no watcher on the bus: the host must take the role")
	}

	item := startItem(t, bus, "app")
	item.register(t)
	items := waitItems(t, store, func(i []Item) bool { return len(i) == 1 })
	got := items[0]
	if got.ID != "app" || got.Bus != item.conn.Names()[0] || got.Path != "/StatusNotifierItem" || got.MenuPath != "/MenuBar" {
		t.Fatalf("tracked = %+v", got)
	}
	if len(got.IconPixmap) != 1 || got.IconPixmap[0].Width != 1 {
		t.Errorf("pixmaps = %+v", got.IconPixmap)
	}
	// The watcher answers the property clients read.
	var regs []string
	if err := item.conn.Object(WatcherName, WatcherPath).Call(PropsIface+".Get", 0, WatcherIface, "RegisteredStatusNotifierItems").Store(&regs); err != nil || len(regs) != 1 {
		t.Errorf("RegisteredStatusNotifierItems = %v, %v", regs, err)
	}

	// NewIcon re-reads the snapshot.
	item.props.SetMust(ItemIface, "IconName", "second-icon")
	if err := item.conn.Emit("/StatusNotifierItem", ItemIface+".NewIcon"); err != nil {
		t.Fatal(err)
	}
	waitItems(t, store, func(i []Item) bool { return len(i) == 1 && i[0].IconName == "second-icon" })

	// The menu: AboutToShow, then the parsed layout.
	actions := host.Actions()
	menu, err := actions.RefreshMenu(context.Background(), got)
	if err != nil {
		t.Fatalf("RefreshMenu: %v", err)
	}
	if len(item.menu.shown) != 1 || item.menu.shown[0] != 0 {
		t.Errorf("AboutToShow calls = %v, want the root", item.menu.shown)
	}
	if len(menu.Children) != 4 {
		t.Fatalf("root children = %d, want 4", len(menu.Children))
	}
	mute, sep, more, hidden := menu.Children[0], menu.Children[1], menu.Children[2], menu.Children[3]
	if mute.Label != "_Mute" || mute.Toggle != ToggleCheckmark || mute.ToggleState != ToggleChecked || !mute.Enabled {
		t.Errorf("mute = %+v", mute)
	}
	if !sep.Separator || hidden.Visible {
		t.Errorf("separator %+v / hidden %+v", sep, hidden)
	}
	if len(more.Children) != 1 || more.Children[0].Label != "Quit" || more.Children[0].Enabled || len(more.Children[0].Shortcut) != 1 {
		t.Errorf("submenu = %+v", more.Children)
	}
	if err := actions.MenuClicked(context.Background(), got, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		item.menu.mu.Lock()
		n := len(item.menu.clicked)
		item.menu.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			if n != 1 {
				t.Error("the clicked event never arrived")
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A LayoutUpdated reaches an open menu's feed, debounced.
	feed, stop := host.WatchMenu(got.Key())
	for range 3 {
		_ = item.conn.Emit("/MenuBar", MenuIface+".LayoutUpdated", uint32(8), int32(0))
	}
	select {
	case <-feed:
	case <-time.After(2 * time.Second):
		t.Fatal("no menu tick for LayoutUpdated")
	}
	stop()

	// The item's connection leaving unregisters it.
	_ = item.conn.Close()
	waitItems(t, store, func(i []Item) bool { return len(i) == 0 })
}

func TestHostRecoversOrphanedItems(t *testing.T) {
	bus := dbustest.Start(t)
	// An item already on the bus from an earlier watcher's era.
	item := startItem(t, bus, "orphan")
	store := NewStore()
	host, err := NewHostOn(bus.Conn(t), store)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	items := waitItems(t, store, func(i []Item) bool { return len(i) == 1 })
	if items[0].ID != "orphan" || items[0].Bus != item.conn.Names()[0] {
		t.Errorf("recovered = %+v", items[0])
	}
}

func TestHostAgainstAnExternalWatcher(t *testing.T) {
	bus := dbustest.Start(t)
	// The first host owns the watcher; a second one hosts against it.
	first, err := NewHostOn(bus.Conn(t), NewStore())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	store := NewStore()
	second, err := NewHostOn(bus.Conn(t), store)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.watcher != nil {
		t.Fatal("the second host took the watcher role from the first")
	}
	item := startItem(t, bus, "shared")
	item.register(t)
	waitItems(t, store, func(i []Item) bool { return len(i) == 1 && i[0].ID == "shared" })
}
