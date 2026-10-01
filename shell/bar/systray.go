package bar

import (
	"context"
	"errors"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/service/sni"
	"github.com/stubbedev/wayle/styling"
)

// errSystrayNoStore guards the wiring: the module needs the shared
// store RunWith owns.
var errSystrayNoStore = errors.New("systray: no SNI store available")

// Tray icon constants (systray/item/helpers.rs).
const (
	// trayPixmapTarget is the size select_best_pixmap aims for.
	trayPixmapTarget = 24
	// trayFallbackIcon is the glyph for an item with no icon at all.
	trayFallbackIcon = "application-x-executable-symbolic"
	trayCallTimeout  = 5 * time.Second
)

// trayIconExtensions are tried, in order, in an item's IconThemePath.
var trayIconExtensions = []string{"png", "svg", "xpm"}

// TrayService is what the tray module drives: the item controls, the
// DBusMenu calls, and the open-menu change feed. The shell wires the
// SNI host; tests fake it.
type TrayService interface {
	Activate(ctx context.Context, it sni.Item) error
	SecondaryActivate(ctx context.Context, it sni.Item) error
	ContextMenu(ctx context.Context, it sni.Item) error
	RefreshMenu(ctx context.Context, it sni.Item) (sni.MenuItem, error)
	MenuClicked(ctx context.Context, it sni.Item, id int32) error
	WatchMenu(key string) (<-chan struct{}, func())
}

// NewTrayService adapts a running SNI host.
func NewTrayService(h *sni.Host) TrayService { return hostTray{h: h, a: h.Actions()} }

// hostTray is the live TrayService; activations carry (0, 0) as the
// Rust tray sends.
type hostTray struct {
	h *sni.Host
	a *sni.Actions
}

func (t hostTray) Activate(ctx context.Context, it sni.Item) error {
	return t.a.Activate(ctx, it, 0, 0)
}

func (t hostTray) SecondaryActivate(ctx context.Context, it sni.Item) error {
	return t.a.SecondaryActivate(ctx, it, 0, 0)
}

func (t hostTray) ContextMenu(ctx context.Context, it sni.Item) error {
	return t.a.ContextMenu(ctx, it, 0, 0)
}

func (t hostTray) RefreshMenu(ctx context.Context, it sni.Item) (sni.MenuItem, error) {
	return t.a.RefreshMenu(ctx, it)
}

func (t hostTray) MenuClicked(ctx context.Context, it sni.Item, id int32) error {
	return t.a.MenuClicked(ctx, it, id)
}

func (t hostTray) WatchMenu(key string) (<-chan struct{}, func()) { return t.h.WatchMenu(key) }

// systrayMatches is helpers.rs's pattern test against an item's Id and
// Title: case-sensitive globs.
func systrayMatches(pattern string, it sni.Item) bool {
	return glob.Wildcard(pattern, it.ID) || glob.Wildcard(pattern, it.Title)
}

// systrayBlacklisted is is_blacklisted.
func systrayBlacklisted(patterns []string, it sni.Item) bool {
	for _, p := range patterns {
		if systrayMatches(p, it) {
			return true
		}
	}
	return false
}

// systrayOverride is find_override: the first entry whose name matches.
func systrayOverride(overrides []config.TrayItemOverride, it sni.Item) (config.TrayItemOverride, bool) {
	for _, o := range overrides {
		if systrayMatches(o.Name, it) {
			return o, true
		}
	}
	return config.TrayItemOverride{}, false
}

// bestPixmap is select_best_pixmap: the one closest to 24x24 by summed
// width and height distance, the first on ties.
func bestPixmap(pixmaps []sni.Pixmap) (sni.Pixmap, bool) {
	best, bestDist := sni.Pixmap{}, math.MaxInt
	for _, p := range pixmaps {
		d := absInt(int(p.Width)-trayPixmapTarget) + absInt(int(p.Height)-trayPixmapTarget)
		if d < bestDist {
			best, bestDist = p, d
		}
	}
	return best, bestDist != math.MaxInt
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// themePathIcon is find_icon_in_theme_path: <path>/<name>.<ext>.
func themePathIcon(themePath, name string) (string, bool) {
	if themePath == "" {
		return "", false
	}
	for _, ext := range trayIconExtensions {
		file := filepath.Join(themePath, name+"."+ext)
		if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() {
			return file, true
		}
	}
	return "", false
}

// isFile reports whether name is a readable regular file path.
func isFile(name string) bool {
	st, err := os.Stat(name)
	return err == nil && st.Mode().IsRegular()
}

// trayIconSource is the resolved source of an item's glyph, and its
// identity for change detection (item/mod.rs IconSignature).
type trayIconSource struct {
	kind   string // "file", "named", "pixmap", "fallback"
	name   string
	pixmap sni.Pixmap
}

// signature identifies the source: equal sources keep the widget.
func (s trayIconSource) signature() string {
	if s.kind == "pixmap" {
		return "pixmap:" + strconv.Itoa(int(s.pixmap.Width)) + "x" + strconv.Itoa(int(s.pixmap.Height)) + ":" + string(s.pixmap.Data)
	}
	return s.kind + ":" + s.name
}

// resolveTrayIcon is update_icon's source pick: the override icon or
// the item's IconName - as a file in IconThemePath, a file path, or a
// theme name - else the best IconPixmap, else the fallback glyph.
func resolveTrayIcon(cfg config.SystrayConfig, it sni.Item) trayIconSource {
	name := it.IconName
	if o, ok := systrayOverride(cfg.Overrides, it); ok && o.Icon != nil && *o.Icon != "" {
		name = *o.Icon
	}
	if name != "" {
		if file, ok := themePathIcon(it.IconThemePath, name); ok {
			return trayIconSource{kind: "file", name: file}
		}
		if isFile(name) {
			return trayIconSource{kind: "file", name: name}
		}
		return trayIconSource{kind: "named", name: name}
	}
	if p, ok := bestPixmap(it.IconPixmap); ok {
		return trayIconSource{kind: "pixmap", pixmap: p}
	}
	return trayIconSource{kind: "fallback", name: trayFallbackIcon}
}

// buildTrayIcon makes the widget for a source at px logical pixels; a
// pixmap that does not decode falls back to the generic glyph.
func buildTrayIcon(src trayIconSource, px int) *widget.Icon {
	switch src.kind {
	case "file":
		return widget.NewFileIcon(src.name, px)
	case "pixmap":
		p := src.pixmap
		if ic, err := render.IconFromARGB32(int(p.Width), int(p.Height), p.Data, px, px); err == nil {
			return widget.NewIcon(ic)
		}
		return widget.NewThemeIcon(trayFallbackIcon, px)
	}
	return widget.NewThemeIcon(src.name, px)
}

// trayButton is one item: its icon in a button taking the three
// clicks (item/mod.rs's root button plus the right/middle gestures).
type trayButton struct {
	widget.Base
	inner    *widget.Button
	onMiddle func()
	onRight  func()
}

func (b *trayButton) Measure(con widget.Constraints) widget.Size { return b.inner.Measure(con) }

func (b *trayButton) Arrange(r render.Rect) {
	b.ArrangeSelf(r)
	widget.SetParents(b, b.inner)
	b.inner.Arrange(r)
}

func (b *trayButton) Paint(cv *render.Canvas) { b.inner.Paint(cv) }

func (b *trayButton) HitTest(p widget.Point) widget.Widget { return b.HitLeaf(b, p) }

// SetHovered implements widget.HoverSetter.
func (b *trayButton) SetHovered(on bool) { b.inner.SetHovered(on) }

// SetPressed implements widget.PressSetter.
func (b *trayButton) SetPressed(on bool) { b.inner.SetPressed(on) }

// ClickAt implements widget.Clicker: the left click.
func (b *trayButton) ClickAt(p widget.Point) { b.inner.ClickAt(p) }

// PointerButton routes the middle and right presses.
func (b *trayButton) PointerButton(button uint32) {
	switch button {
	case widget.BTNMiddle:
		b.onMiddle()
	case widget.BTNRight:
		b.onRight()
	}
}

// trayEntry is one rendered item.
type trayEntry struct {
	item   sni.Item
	button *trayButton
	slot   *fixedBox
	icon   *widget.Icon
	sig    string
}

// systrayModule is the tray: one button per SNI item, following the
// store, with DBusMenu menus in a popover.
type systrayModule struct {
	ctx   ModuleContext
	store *sni.Store
	tray  TrayService
	host  app.Host

	entries map[string]*trayEntry
	root    *widget.Box

	// The open menu: its item key, popover, and stack.
	menuKey string
	menuPop *app.MenuPopover
	// menuItems are the rows last built, kept headless (no popover).
	menuItems []widget.MenuItem
}

func newSystray(ctx ModuleContext) (Module, error) {
	if ctx.SNI == nil {
		return nil, errSystrayNoStore
	}
	m := &systrayModule{
		ctx:     ctx,
		store:   ctx.SNI,
		tray:    ctx.Tray,
		entries: make(map[string]*trayEntry),
		root:    widget.NewBox(widget.Row, trayItemGap(ctx), 0),
	}
	m.root.AddClass("systray")
	ticks, stop := ctx.SNI.Subscribe()
	follow(ctx, ticks, stop, func(struct{}) { m.refresh() })
	m.refresh()
	return m, nil
}

// trayItemGap is item-gap in pixels (resolved at 1 rem).
func trayItemGap(ctx ModuleContext) int {
	return int(math.Round(ctx.Config.Systray.ItemGap.ResolvePx(styling.RemBase, float64(ctx.Config.Bar.Scale))))
}

// iconPx is icon-scale resolved at the 1.25 rem base.
func (m *systrayModule) iconPx() int {
	return int(math.Round(m.ctx.Config.Systray.IconScale.ResolvePx(config.SystrayIconBaseRem*styling.RemBase, float64(m.ctx.Config.Bar.Scale))))
}

// Attach records the layer the menus open on.
func (m *systrayModule) Attach(host app.Host) { m.host = host }

// ownsChrome keeps the tray out of the module-button wrapper: each
// item is its own button.
func (m *systrayModule) ownsChrome() {}

// refresh reconciles the buttons with the store (update_items): the
// blacklist hides items, the rest keep registration order, and each
// icon rebuilds only when its source changed.
func (m *systrayModule) refresh() {
	cfg := m.ctx.Config.Systray
	live := make(map[string]bool)
	var order []*trayEntry
	for _, it := range m.store.Items() {
		if systrayBlacklisted(cfg.Blacklist, it) {
			continue
		}
		key := it.Key()
		live[key] = true
		e, ok := m.entries[key]
		if !ok {
			e = m.newEntry(it)
			m.entries[key] = e
		}
		e.item = it
		m.updateIcon(e)
		order = append(order, e)
	}
	for key := range m.entries {
		if !live[key] {
			delete(m.entries, key)
			if key == m.menuKey {
				m.closeMenu()
			}
		}
	}
	m.root.Clear()
	for _, e := range order {
		m.root.Append(e.button, false)
	}
	m.root.SetVisible(len(order) > 0)
}

// newEntry builds one item's button and wires its clicks.
func (m *systrayModule) newEntry(it sni.Item) *trayEntry {
	e := &trayEntry{item: it}
	px := m.iconPx()
	e.slot = newFixedBox(px, px, nil)
	inner := widget.NewButton(e.slot, 2, 4)
	inner.AddClass("systray-item")
	if m.ctx.Style != nil {
		inner.BgHover = m.ctx.Style.buttonBgHover
		inner.BgPressed = m.ctx.Style.buttonBgActive
	}
	key := it.Key()
	inner.OnClick = func() { m.leftClick(key) }
	e.button = &trayButton{
		inner:    inner,
		onMiddle: func() { m.middleClick(key) },
		onRight:  func() { m.rightClick(key) },
	}
	return e
}

// updateIcon applies the resolved source and the override tint.
func (m *systrayModule) updateIcon(e *trayEntry) {
	cfg := m.ctx.Config.Systray
	src := resolveTrayIcon(cfg, e.item)
	if sig := src.signature(); sig != e.sig || e.icon == nil {
		e.icon = buildTrayIcon(src, m.iconPx())
		e.slot.SetChild(e.icon)
		e.sig = sig
	}
	var tint render.Color
	if o, ok := systrayOverride(cfg.Overrides, e.item); ok && o.Color != nil {
		tint, _ = styling.ResolveColor(*o.Color, m.ctx.Style.palette)
	}
	e.icon.SetTint(tint)
}

// call runs one item control off the loop, logging failures as the
// Rust tray's warn! does.
func (m *systrayModule) call(what string, key string, fn func(ctx context.Context, it sni.Item) error) {
	e, ok := m.entries[key]
	if !ok || m.tray == nil {
		return
	}
	it := e.item
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), trayCallTimeout)
		defer cancel()
		if err := fn(ctx, it); err != nil {
			log.Printf("systray: %s %s: %v", it.ID, what, err)
		}
	}()
}

// leftClick activates the item, or asks for its own menu when the item
// is menu-only (ItemIsMenu).
func (m *systrayModule) leftClick(key string) {
	e, ok := m.entries[key]
	if !ok || m.tray == nil {
		return
	}
	if e.item.ItemIsMenu {
		m.call("context menu", key, m.tray.ContextMenu)
		return
	}
	m.call("activate", key, m.tray.Activate)
}

// middleClick is SecondaryActivate.
func (m *systrayModule) middleClick(key string) {
	if m.tray != nil {
		m.call("secondary activate", key, m.tray.SecondaryActivate)
	}
}

// rightClick is request_menu_show: a second right-click closes the
// open menu; otherwise AboutToShow and a fresh layout, then the menu -
// or the item's own ContextMenu when it publishes none.
func (m *systrayModule) rightClick(key string) {
	if m.menuKey == key && m.menuPop != nil && !m.menuPop.Closed() {
		m.closeMenu()
		return
	}
	e, ok := m.entries[key]
	if !ok || m.tray == nil {
		return
	}
	it := e.item
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), trayCallTimeout)
		defer cancel()
		menu, err := m.tray.RefreshMenu(ctx, it)
		m.ctx.Invoke(func() { m.showMenu(key, menu, err) })
	}()
}

// showMenu opens the popover over the item, or falls back to the
// item's ContextMenu for an absent or empty menu.
func (m *systrayModule) showMenu(key string, menu sni.MenuItem, err error) {
	e, ok := m.entries[key]
	if !ok {
		return
	}
	if err != nil || len(menu.Children) == 0 {
		m.call("context menu", key, m.tray.ContextMenu)
		return
	}
	font, px := dropdownFont(m.ctx)
	rows := m.menuRows(e.item, menu.Children)
	if m.ctx.App == nil || m.host == nil {
		m.menuKey, m.menuItems = key, rows
		return
	}
	m.closeMenu()
	feed, stop := m.tray.WatchMenu(key)
	// Submenus open as their own popovers beside their rows, as the
	// Rust tray's PopoverMenu with the NESTED flag does.
	pop, err := m.ctx.App.OpenMenuPopover(m.host, app.MenuPopoverConfig{
		Anchor:   e.button,
		Face:     font,
		SizePx:   px,
		Items:    rows,
		Serial:   m.ctx.App.LastPressSerial(m.host),
		OnClosed: stop,
	})
	if err != nil {
		stop()
		log.Printf("systray: %s menu: %v", e.item.ID, err)
		return
	}
	m.menuKey, m.menuPop, m.menuItems = key, pop, rows
	it := e.item
	go func() {
		for range feed {
			ctx, cancel := context.WithTimeout(context.Background(), trayCallTimeout)
			fresh, err := m.tray.RefreshMenu(ctx, it)
			cancel()
			if err != nil {
				continue
			}
			m.ctx.Invoke(func() {
				if m.menuPop == pop {
					pop.SetItems(m.menuRows(it, fresh.Children)...)
				}
			})
		}
	}()
}

// closeMenu dismisses the open menu.
func (m *systrayModule) closeMenu() {
	if m.menuPop != nil {
		m.menuPop.Dismiss()
	}
	m.menuKey, m.menuPop, m.menuItems = "", nil, nil
}

// menuRows is the gtk4 adapter's build_model: invisible rows drop,
// separators split sections (never leading, trailing, or doubled),
// labels lose their leading mnemonic underscores, toggles become
// check/radio rows, disabled rows stay visible but inert, submenus
// nest, and every leaf sends the clicked event.
func (m *systrayModule) menuRows(it sni.Item, nodes []sni.MenuItem) []widget.MenuItem {
	_, px := dropdownFont(m.ctx)
	var rows []widget.MenuItem
	pendingSep := false
	for _, n := range nodes {
		if !n.Visible {
			continue
		}
		if n.Separator {
			pendingSep = len(rows) > 0
			continue
		}
		if pendingSep {
			rows = append(rows, widget.MenuSeparator())
			pendingSep = false
		}
		row := widget.MenuItem{
			Label:    strings.TrimLeft(n.Label, "_"),
			Disabled: !n.Enabled,
			Accel:    trayAccel(n.Shortcut),
			Icon:     trayMenuIcon(n, int(px)),
		}
		if len(n.Children) > 0 {
			row.Items = m.menuRows(it, n.Children)
			rows = append(rows, row)
			continue
		}
		switch n.Toggle {
		case sni.ToggleCheckmark:
			row.Kind, row.Checked = widget.ItemCheck, n.ToggleState == sni.ToggleChecked
		case sni.ToggleRadio:
			row.Kind, row.Checked, row.Group = widget.ItemRadio, n.ToggleState == sni.ToggleChecked, "radio"
		}
		id := n.ID
		row.OnClick = func() {
			m.call("menu event", it.Key(), func(ctx context.Context, item sni.Item) error {
				return m.tray.MenuClicked(ctx, item, id)
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// trayMenuIcon is a row's icon: icon-name from the theme, else the PNG
// icon-data; nil without either.
func trayMenuIcon(n sni.MenuItem, px int) *widget.Icon {
	if n.IconName != "" {
		return widget.NewThemeIcon(n.IconName, px)
	}
	if len(n.IconData) > 0 {
		if ic, err := render.LoadPNG(n.IconData, px, px); err == nil {
			return widget.NewIcon(ic)
		}
	}
	return nil
}

// trayAccel renders the first shortcut as an accelerator label
// (to_gtk_accelerator's modifier set: Control, Shift, Alt, Super; other
// modifiers drop).
func trayAccel(shortcut [][]string) string {
	if len(shortcut) == 0 || len(shortcut[0]) == 0 {
		return ""
	}
	keys := shortcut[0]
	var parts []string
	for _, mod := range keys[:len(keys)-1] {
		switch mod {
		case "Control":
			parts = append(parts, "Ctrl")
		case "Shift", "Alt", "Super":
			parts = append(parts, mod)
		}
	}
	return strings.Join(append(parts, keys[len(keys)-1]), "+")
}

func (m *systrayModule) Root() widget.Widget { return m.root }
