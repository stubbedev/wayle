package bar

import (
	"errors"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/hyprland"
)

// windowTitleLabel is helpers.rs's format_label: the template over
// title/app, falling back to the _bar.ftl "Desktop" when the render is
// blank.
func windowTitleLabel(format, title, appID string) string {
	label := format
	label = replaceTemplateVar(label, "title", title)
	label = replaceTemplateVar(label, "app", appID)
	if strings.TrimSpace(label) == "" {
		return i18n.T("bar-window-title-empty")
	}
	return label
}

// windowTitle is the module: the focused window's title.
type windowTitleModule struct {
	ctx   ModuleContext
	conn  *hyprland.Connection
	label *widget.Label
	title string
	appID string
}

func newWindowTitle(ctx ModuleContext) (Module, error) {
	if ctx.Hyprland == nil {
		return nil, errors.New("window-title: the compositor is not Hyprland")
	}
	m := &windowTitleModule{ctx: ctx, conn: ctx.Hyprland}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	events, err := ctx.Hyprland.Events(ctx.Life())
	if err != nil {
		return nil, err
	}
	follow(ctx, events, nil, func(event hyprland.Event) {
		if event.Kind != hyprland.EventActiveWindow {
			return
		}
		m.title, m.appID = event.Title, event.Class
		m.render()
	})
	return m, nil
}

// render applies the current title/app state to the label.
func (m *windowTitleModule) render() {
	cfg := m.ctx.Config.WindowTitle
	text := ""
	if cfg.LabelShow {
		text = windowTitleLabel(cfg.Format, m.title, m.appID)
	}
	m.label.SetText(text)
}

func (m *windowTitleModule) Root() widget.Widget { return m.label }
