package bar

import (
	"errors"
	"fmt"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/styling"
)

// notificationLabel is helpers.rs's format_label: the count
// zero-padded to two digits.
func notificationLabel(count int) string {
	return fmt.Sprintf("%02d", count)
}

// notificationIconName is helpers.rs's select_icon: dnd wins, then
// the unread dot, then the resting bell.
func notificationIconName(cfg config.NotificationConfig, count int, dnd bool) string {
	if dnd {
		return cfg.IconDnd
	}
	if count > 0 {
		return cfg.IconUnread
	}
	return cfg.Icon.Name
}

// notificationColor resolves the count's threshold color, the bar fg
// otherwise.
func notificationColor(cfg config.NotificationConfig, count int, palette *styling.Palette, fallback render.Color) render.Color {
	if override, ok := thresholdColor(float64(count), cfg.Thresholds, palette); ok {
		return override
	}
	return fallback
}

// notificationModule is the bell: count and dnd from the shared
// notification service.
type notificationModule struct {
	ctx   ModuleContext
	src   *notifications.Service
	label *widget.Label
	icon  widget.Widget
	root  widget.Widget
}

func newNotification(ctx ModuleContext) (Module, error) {
	m := &notificationModule{ctx: ctx, src: ctx.Notifications, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	if m.src == nil {
		return nil, errors.New("notifications: no notification service available")
	}
	m.icon = moduleIcon(ctx, ctx.Config.Notification.Icon)
	m.root = assembleModule(ctx, ctx.Config.Notification.Icon, m.label)
	m.refresh()
	// Follow the service's change feed; the events also drive the OSD
	// popups later. Headless construction refreshes inline.
	if ctx.App == nil {
		go func() {
			for range m.src.Events() {
				m.refresh()
			}
		}()
		return m, nil
	}
	go func() {
		for range m.src.Events() {
			m.ctx.Invoke(m.refresh)
		}
	}()
	return m, nil
}

// refresh applies the count and dnd state to label and icon.
func (m *notificationModule) refresh() {
	cfg := m.ctx.Config.Notification
	count := m.src.Count()
	dnd := m.src.DND()
	text := ""
	if cfg.LabelShow {
		text = notificationLabel(count)
	}
	m.label.SetText(text)
	m.label.SetColor(notificationColor(cfg, count, m.ctx.Style.palette, m.ctx.Style.fg))
	if setter, ok := m.icon.(interface{ SetThemeName(name string) }); ok {
		setter.SetThemeName(notificationIconName(cfg, count, dnd))
	}
}

func (m *notificationModule) Root() widget.Widget { return m.root }

// RunAction handles the dnd toggle the Rust module exposes through
// `wayle notify dnd`; the shell command path routes here.
func (m *notificationModule) RunAction(action config.ClickAction) {
	if action.Command == "toggle-dnd" {
		m.src.ToggleDND()
		return
	}
	runClickAction(m.ctx, action)
}
