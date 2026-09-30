package bar

import (
	"context"
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/hyprland"
)

// keybindModeLabel is helpers.rs's format_label: an empty submap
// renders the "default" vocabulary word through the mode placeholder.
func keybindModeLabel(format, submap string) string {
	mode := submap
	if mode == "" {
		mode = "default"
	}
	return replaceTemplateVar(format, "mode", mode)
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
	m.icon = moduleIcon(ctx, ctx.Config.KeybindMode.Icon)
	if m.icon != nil {
		row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
		row.Append(m.icon, false)
		row.Append(m.label, false)
		m.root = row
	} else {
		m.root = m.label
	}
	events, err := ctx.Hyprland.Events(context.Background())
	if err != nil {
		return nil, err
	}
	go func() {
		for event := range events {
			if event.Kind != hyprland.EventSubmap {
				continue
			}
			apply := func() {
				m.submap = event.Name
				m.render()
			}
			if m.ctx.App != nil {
				m.ctx.Invoke(apply)
			} else {
				apply()
			}
		}
	}()
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
