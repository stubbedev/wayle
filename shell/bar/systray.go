package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/glob"
	"github.com/stubbedev/wayle/service/sni"
)

// errSystrayNoStore guards the wiring: the module needs the shared
// store RunWith owns.
var errSystrayNoStore = errors.New("systray: no SNI store available")

// systrayModule is the tray: one icon per SNI item, following the
// store's change feed.
type systrayModule struct {
	ctx   ModuleContext
	store *sni.Store

	boxes map[string]widget.Widget
	root  *widget.Box
}

func newSystray(ctx ModuleContext) (Module, error) {
	if ctx.SNI == nil {
		return nil, errSystrayNoStore
	}
	m := &systrayModule{
		ctx:   ctx,
		store: ctx.SNI,
		boxes: make(map[string]widget.Widget),
		root:  widget.NewBox(widget.Row, ctx.Style.moduleGap, 0),
	}
	if ctx.App == nil {
		// Headless construction renders the resting (empty) tray; the
		// host connection belongs to RunWith.
		return m, nil
	}
	go func() {
		for range ctx.SNI.Changes() {
			m.ctx.Invoke(m.refresh)
		}
	}()
	m.refresh()
	return m, nil
}

// refresh reconciles the icon row with the store's items.
func (m *systrayModule) refresh() {
	cfg := m.ctx.Config.Systray
	items := m.store.Items()
	live := make(map[string]bool, len(items))
	for _, it := range items {
		if systrayBlacklisted(cfg.Blacklist, it.ID, it.Title) {
			continue
		}
		live[it.Key()] = true
		if _, ok := m.boxes[it.Key()]; !ok {
			icon := widget.NewThemeIcon(systrayIconName(cfg, it), m.iconPx())
			m.boxes[it.Key()] = icon
			m.root.Append(icon, false)
		}
	}
	for key, box := range m.boxes {
		if !live[key] {
			m.root.Remove(box)
			delete(m.boxes, key)
		}
	}
	m.root.InvalidateLayout()
}

// systrayIconName resolves the item's glyph: the config override, then
// the attention icon (NeedsAttention), then the plain icon name.
// Pixmap-only icons wait for the raster pass; name-less items get a
// placeholder.
func systrayIconName(cfg config.SystrayConfig, it sni.Item) string {
	if override, ok := cfg.Overrides[it.ID]; ok {
		return override
	}
	if it.Status == sni.StatusNeedsAttention && it.AttentionName != "" {
		return it.AttentionName
	}
	if it.IconName != "" {
		return it.IconName
	}
	return "ld-package-symbolic"
}

// iconPx applies the scale knob to the module's icon size.
func (m *systrayModule) iconPx() int {
	size := float64(m.ctx.Config.Systray.IconSize) * m.ctx.Config.Systray.IconScale
	return int(size)
}

func systrayBlacklisted(patterns []string, id, title string) bool {
	for _, pattern := range patterns {
		if glob.Match(pattern, id) || glob.Match(pattern, title) {
			return true
		}
	}
	return false
}

func (m *systrayModule) Root() widget.Widget { return m.root }
