package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/hyprland"
)

// keybindModeLabel is helpers.rs's format_label: an empty submap
// renders the "default" vocabulary word through the mode placeholder.
func keybindModeLabel(format, submap string) string {
	mode := submap
	if mode == "" {
		mode = i18n.T("bar-keybind-mode-default")
	}
	return jinja.RenderOr(format, map[string]any{"mode": mode})
}

// keybindModeVisible is helpers.rs's compute_visibility: auto-hide
// collapses the module on the default submap.
func keybindModeVisible(submap string, autoHide bool) bool {
	return !autoHide || submap != ""
}

// keybindMode is the module: the active Hyprland submap.
type keybindModeModule struct {
	ctx    ModuleContext
	root   widget.Widget
	label  *widget.Label
	icon   *widget.Icon
	submap string
}

func newKeybindMode(ctx ModuleContext) (Module, error) {
	if ctx.Hyprland == nil {
		return nil, errors.New("keybind-mode: the compositor is not Hyprland")
	}
	m := &keybindModeModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.KeybindMode.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	events, err := ctx.Hyprland.Events(ctx.Life())
	if err != nil {
		return nil, err
	}
	follow(ctx, events, nil, func(event hyprland.Event) {
		if event.Kind != hyprland.EventSubmap {
			return
		}
		m.submap = event.Name
		m.render()
	})
	return m, nil
}

// render applies the current submap to the label and, under auto-hide,
// the whole module's visibility.
func (m *keybindModeModule) render() {
	cfg := m.ctx.Config.KeybindMode
	text := ""
	if cfg.LabelShow {
		text = keybindModeLabel(cfg.Format, m.submap)
	}
	m.label.SetText(text)
	visible := keybindModeVisible(m.submap, cfg.AutoHide)
	m.label.SetVisible(visible)
	if setter := m.icon; setter != nil {
		setter.SetVisible(visible)
	}
}

func (m *keybindModeModule) Root() widget.Widget { return m.root }
