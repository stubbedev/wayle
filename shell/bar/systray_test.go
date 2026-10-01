package bar

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sni"
)

// fakeTray records the tray calls and serves a scripted menu.
type fakeTray struct {
	mu    sync.Mutex
	calls []string
	menu  sni.MenuItem
	err   error
}

func (f *fakeTray) record(s string) error {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
	return nil
}

func (f *fakeTray) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeTray) Activate(_ context.Context, it sni.Item) error {
	return f.record("activate " + it.ID)
}

func (f *fakeTray) SecondaryActivate(_ context.Context, it sni.Item) error {
	return f.record("secondary " + it.ID)
}

func (f *fakeTray) ContextMenu(_ context.Context, it sni.Item) error {
	return f.record("context-menu " + it.ID)
}

func (f *fakeTray) RefreshMenu(_ context.Context, it sni.Item) (sni.MenuItem, error) {
	_ = f.record("refresh " + it.ID)
	return f.menu, f.err
}

func (f *fakeTray) MenuClicked(_ context.Context, it sni.Item, id int32) error {
	return f.record("clicked " + it.ID + " " + strconv.Itoa(int(id)))
}

func (f *fakeTray) WatchMenu(string) (<-chan struct{}, func()) {
	return make(chan struct{}), func() {}
}

func waitTray(t *testing.T, f *fakeTray, n int) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(f.Calls()) < n && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	return f.Calls()
}

func newTrayForTest(t *testing.T, cfg *config.Config, items ...*sni.Item) (*systrayModule, *fakeTray, *sni.Store) {
	t.Helper()
	store := sni.NewStore()
	for _, it := range items {
		store.Put(it)
	}
	tray := &fakeTray{}
	ctx := newTestContext(t, cfg)
	ctx.SNI, ctx.Tray = store, tray
	module, err := Create("systray", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return module.(*systrayModule), tray, store
}

func TestSystrayPatternsAreCaseSensitiveGlobs(t *testing.T) {
	it := sni.Item{ID: "com.discord.Discord", Title: "Discord"}
	if !systrayBlacklisted([]string{"*discord*"}, it) {
		t.Error("*discord* should match the id")
	}
	if !systrayBlacklisted([]string{"Discord"}, it) {
		t.Error("an exact title should match")
	}
	if systrayBlacklisted([]string{"*DISCORD*"}, it) {
		t.Error("patterns are case-sensitive")
	}
	o, ok := systrayOverride([]config.TrayItemOverride{{Name: "nope"}, {Name: "*Discord", Icon: new("x")}}, it)
	if !ok || o.Icon == nil || *o.Icon != "x" {
		t.Errorf("override = %+v, %v", o, ok)
	}
}

func TestBestPixmap(t *testing.T) {
	px := func(w, h int32) sni.Pixmap { return sni.Pixmap{Width: w, Height: h} }
	for _, tc := range []struct {
		in   []sni.Pixmap
		want int32
	}{
		{[]sni.Pixmap{px(16, 16), px(24, 24), px(48, 48)}, 24},
		{[]sni.Pixmap{px(16, 16), px(28, 28), px(64, 64)}, 28},
		{[]sni.Pixmap{px(8, 8), px(20, 20)}, 20},
		{[]sni.Pixmap{px(128, 128)}, 128},
		{[]sni.Pixmap{px(32, 16), px(24, 24), px(16, 32)}, 24},
	} {
		if got, ok := bestPixmap(tc.in); !ok || got.Width != tc.want {
			t.Errorf("%v = %v, want width %d", tc.in, got, tc.want)
		}
	}
	if _, ok := bestPixmap(nil); ok {
		t.Error("no pixmaps selected one")
	}
}

func TestResolveTrayIcon(t *testing.T) {
	cfg := config.DefaultsSystray()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-icon.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	pixmap := sni.Pixmap{Width: 1, Height: 1, Data: []byte{0xff, 0xff, 0, 0}}
	for _, tc := range []struct {
		name string
		it   sni.Item
		cfg  config.SystrayConfig
		kind string
		want string
	}{
		{"theme path file", sni.Item{IconName: "app-icon", IconThemePath: dir}, cfg, "file", filepath.Join(dir, "app-icon.png")},
		{"named", sni.Item{IconName: "nm-applet"}, cfg, "named", "nm-applet"},
		{"file path", sni.Item{IconName: filepath.Join(dir, "app-icon.png")}, cfg, "file", filepath.Join(dir, "app-icon.png")},
		{"pixmap", sni.Item{IconPixmap: []sni.Pixmap{pixmap}}, cfg, "pixmap", ""},
		{"fallback", sni.Item{}, cfg, "fallback", trayFallbackIcon},
		{"override icon wins", sni.Item{ID: "app", IconName: "nm-applet"}, config.SystrayConfig{Overrides: []config.TrayItemOverride{{Name: "app", Icon: new("custom")}}}, "named", "custom"},
	} {
		got := resolveTrayIcon(tc.cfg, tc.it)
		if got.kind != tc.kind || (tc.want != "" && got.name != tc.want) {
			t.Errorf("%s: = %+v, want %s %q", tc.name, got, tc.kind, tc.want)
		}
	}
	// A pixmap icon is a static raster at the icon size.
	icon := buildTrayIcon(trayIconSource{kind: "pixmap", pixmap: pixmap}, 20)
	if sz := icon.Measure(widget.Constraints{Max: widget.Size{W: 100, H: 100}}); sz.W != 20 {
		t.Errorf("pixmap icon = %v, want 20px", sz)
	}
	// A short pixmap is the fallback glyph, not a crash.
	if icon := buildTrayIcon(trayIconSource{kind: "pixmap", pixmap: sni.Pixmap{Width: 4, Height: 4}}, 20); icon.Name() != trayFallbackIcon {
		t.Errorf("short pixmap = %q, want the fallback", icon.Name())
	}
}

func TestSystrayClicksDriveTheItem(t *testing.T) {
	cfg := config.Defaults()
	plain := &sni.Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "plain"}
	menuOnly := &sni.Item{Bus: ":1.2", Path: "/StatusNotifierItem", ID: "menu-only", ItemIsMenu: true}
	m, tray, _ := newTrayForTest(t, cfg, plain, menuOnly)
	if got := len(m.root.Children()); got != 2 {
		t.Fatalf("buttons = %d, want 2", got)
	}
	m.entries[plain.Key()].button.ClickAt(widget.Point{})
	m.entries[menuOnly.Key()].button.ClickAt(widget.Point{})
	m.entries[plain.Key()].button.PointerButton(widget.BTNMiddle)
	calls := waitTray(t, tray, 3)
	want := map[string]bool{"activate plain": true, "context-menu menu-only": true, "secondary plain": true}
	for _, c := range calls {
		if !want[c] {
			t.Errorf("unexpected call %q", c)
		}
		delete(want, c)
	}
	if len(want) != 0 {
		t.Errorf("missing %v", want)
	}
}

func TestSystrayMenuRows(t *testing.T) {
	cfg := config.Defaults()
	it := &sni.Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "app", MenuPath: "/MenuBar"}
	m, tray, _ := newTrayForTest(t, cfg, it)
	nodes := []sni.MenuItem{
		{ID: 9, Separator: true, Visible: true}, // a leading separator drops
		{ID: 1, Label: "_Mute", Visible: true, Enabled: true, Toggle: sni.ToggleCheckmark, ToggleState: sni.ToggleChecked},
		{ID: 2, Separator: true, Visible: true},
		{ID: 3, Separator: true, Visible: true}, // doubled separators collapse
		{ID: 4, Label: "More", Visible: true, Enabled: true, Children: []sni.MenuItem{
			{ID: 5, Label: "Quit", Visible: true, Enabled: false, Shortcut: [][]string{{"Control", "q"}}},
		}},
		{ID: 6, Label: "Hidden", Visible: false, Enabled: true},
		{ID: 7, Separator: true, Visible: true}, // a trailing separator drops
	}
	rows := m.menuRows(*it, nodes)
	if len(rows) != 3 {
		t.Fatalf("rows = %d (%+v), want mute, separator, more", len(rows), rows)
	}
	if rows[0].Label != "Mute" || rows[0].Kind != widget.ItemCheck || !rows[0].Checked {
		t.Errorf("mute = %+v", rows[0])
	}
	if rows[1].Kind != widget.ItemSeparator {
		t.Errorf("row 1 = %+v, want the section separator", rows[1])
	}
	quit := rows[2].Items[0]
	if !quit.Disabled || quit.Accel != "Ctrl+q" {
		t.Errorf("quit = %+v, want disabled with Ctrl+q", quit)
	}
	rows[0].OnClick()
	if calls := waitTray(t, tray, 1); len(calls) != 1 || calls[0] != "clicked app 1" {
		t.Errorf("calls = %v, want the clicked event for id 1", calls)
	}
}

func TestSystrayRightClickFallsBackWithoutAMenu(t *testing.T) {
	cfg := config.Defaults()
	it := &sni.Item{Bus: ":1.1", Path: "/StatusNotifierItem", ID: "bare"}
	m, tray, _ := newTrayForTest(t, cfg, it)
	// An empty menu: the item's own ContextMenu.
	m.showMenu(it.Key(), sni.MenuItem{}, nil)
	if calls := waitTray(t, tray, 1); len(calls) != 1 || calls[0] != "context-menu bare" {
		t.Errorf("calls = %v, want the context-menu fallback", calls)
	}
	// A real menu builds the stack (headless: no popover).
	m.showMenu(it.Key(), sni.MenuItem{Children: []sni.MenuItem{{ID: 1, Label: "Open", Visible: true, Enabled: true}}}, nil)
	if m.menuStack == nil || m.menuKey != it.Key() {
		t.Error("a published menu built no stack")
	}
}

func TestSystrayFollowsTheStore(t *testing.T) {
	cfg := config.Defaults()
	cfg.Systray.Blacklist = []string{"hidden*"}
	m, _, store := newTrayForTest(t, cfg)
	if m.root.Visible() {
		t.Error("an empty tray should hide")
	}
	store.Put(&sni.Item{Bus: ":1.1", Path: "/a", ID: "shown", IconName: "one"})
	store.Put(&sni.Item{Bus: ":1.2", Path: "/b", ID: "hidden-app"})
	waitHeadless(t, "the one button not blacklisted", func() bool { return len(m.root.Children()) == 1 })
	if !onHeadlessLoop(m.root.Visible) {
		t.Fatal("a tray with a button should show")
	}
}

func TestLoadFileAppliesSystray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	good := "[modules.systray]\nicon-scale = 1.5\nitem-gap = \"6px\"\nblacklist = [\"*steam*\"]\n" +
		"[[modules.systray.overrides]]\nname = \"*discord*\"\nicon = \"discord-symbolic\"\ncolor = \"red\"\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	s := c.Systray
	if s.IconScale.Value != 1.5 || s.ItemGap.Unit != config.SizePixels || len(s.Blacklist) != 1 {
		t.Errorf("systray = %+v", s)
	}
	if len(s.Overrides) != 1 || s.Overrides[0].Icon == nil || *s.Overrides[0].Icon != "discord-symbolic" || s.Overrides[0].Color == nil {
		t.Errorf("overrides = %+v", s.Overrides)
	}
	for _, bad := range []string{
		"[[modules.systray.overrides]]\nicon = \"x\"\n",
		"[[modules.systray.overrides]]\nname = \"x\"\ncolor = \"bogus\"\n",
	} {
		if err := osWrite(path, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}
