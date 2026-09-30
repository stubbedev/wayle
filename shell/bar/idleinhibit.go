package bar

import (
	"strconv"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/styling"
)

// idleInhibitLabel is helpers.rs's build_label: On/Off state, the
// remaining time (H:MM:SS or M:SS, "∞" when indefinite, "-" when
// off), and the stored duration ("∞" when indefinite).
func idleInhibitLabel(format string, active bool, durationMins uint32, remainingSecs int) string {
	state := "Off"
	if active {
		state = "On"
	}
	var remaining string
	switch {
	case !active:
		remaining = "-"
	case durationMins == 0:
		remaining = "∞"
	default:
		remaining = idleinhibit.FormatDuration(remainingSecs)
	}
	duration := "∞"
	if durationMins != 0 {
		duration = strconv.FormatUint(uint64(durationMins), 10)
	}
	out := replaceTemplateVar(format, "state", state)
	out = replaceTemplateVar(out, "remaining", remaining)
	out = replaceTemplateVar(out, "duration", duration)
	return out
}

// idleInhibitColor resolves the state's configured color, the bar fg
// otherwise.
func idleInhibitColor(cfg config.IdleInhibitConfig, active bool, palette *styling.Palette, fallback render.Color) render.Color {
	name := config.IdleInhibitInactive
	if active {
		name = config.IdleInhibitActive
	}
	color, ok := cfg.Colors[name]
	if !ok {
		return fallback
	}
	if resolved, ok := styling.ResolveColor(color, palette); ok {
		return resolved
	}
	return fallback
}

// idleInhibit is the module: the shared inhibit state rendered into
// the bar, with the Wayland inhibitor bound to the bar surface.
type idleInhibitModule struct {
	ctx     ModuleContext
	state   *idleinhibit.State
	label   *widget.Label
	icon    *widget.Icon
	root    widget.Widget
	host    app.Host
	release func()
}

func newIdleInhibit(ctx ModuleContext) (Module, error) {
	state := ctx.IdleInhibit
	if state == nil {
		state = idleinhibit.NewState(ctx.Config.IdleInhibit.StartupDuration)
	}
	m := &idleInhibitModule{ctx: ctx, state: state, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.IdleInhibit.Icons[config.IdleInhibitInactive])
	if m.icon != nil {
		row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
		row.Append(m.icon, false)
		row.Append(m.label, false)
		m.root = row
	} else {
		m.root = m.label
	}
	m.render()
	// Follow the shared state: every flip re-syncs the inhibitor and
	// re-renders.
	go func() {
		for range state.Changes() {
			apply := m.syncInhibitor
			if m.ctx.App != nil {
				m.ctx.Invoke(apply)
			} else {
				apply()
			}
		}
	}()
	return m, nil
}

// Attach binds the inhibitor lifecycle to a host surface. The bar
// calls it once the layer window exists; the module is inert before
// that. The state-following loop starts at construction and rides the
// same notifications.
func (m *idleInhibitModule) Attach(host app.Host) {
	if m.ctx.App == nil || !m.ctx.App.IdleInhibitAvailable() {
		return
	}
	m.ctx.Invoke(func() {
		m.host = host
		m.syncInhibitor()
	})
}

// syncInhibitor matches the live inhibitor to the shared state, then
// repaints. Runs on the loop goroutine.
func (m *idleInhibitModule) syncInhibitor() {
	active := m.state.Active()
	switch {
	case active && m.release == nil && m.host != nil:
		if r, err := m.ctx.App.InhibitIdle(m.host); err == nil {
			m.release = r
		}
	case !active && m.release != nil:
		m.release()
		m.release = nil
	}
	m.render()
}

// render applies the state to label and icon.
func (m *idleInhibitModule) render() {
	cfg := m.ctx.Config.IdleInhibit
	snap := m.state.Status()
	text := ""
	if cfg.LabelShow {
		text = idleInhibitLabel(cfg.Format, snap.Active, snap.DurationMins, snap.RemainingS)
	}
	m.label.SetText(text)
	m.label.SetColor(idleInhibitColor(cfg, snap.Active, m.ctx.Style.palette, m.ctx.Style.fg))
	if setter := m.icon; setter != nil {
		icon := cfg.Icons[config.IdleInhibitInactive]
		if snap.Active {
			icon = cfg.Icons[config.IdleInhibitActive]
		}
		setter.SetThemeName(icon.Name)
	}
	if setter := m.icon; setter != nil {
		setter.SetTint(idleInhibitColor(cfg, snap.Active, m.ctx.Style.palette, m.ctx.Style.fg))
	}
}

func (m *idleInhibitModule) Root() widget.Widget { return m.root }

// Stop releases the inhibitor when the bar goes down.
func (m *idleInhibitModule) Stop() {
	if m.release != nil {
		m.release()
		m.release = nil
	}
}
