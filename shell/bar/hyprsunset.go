package bar

import (
	"log"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// hyprsunsetLabel is helpers.rs's build_label: On/Off status with the
// live temp and gamma while running, "--" placeholders when off.
func hyprsunsetLabel(format string, enabled bool, temp, gamma, configTemp, configGamma int) string {
	status, tempText, gammaText := "Off", "--", "--"
	if enabled {
		status, tempText, gammaText = "On", strconv.Itoa(temp), strconv.Itoa(gamma)
	}
	out := replaceTemplateVar(format, "status", status)
	out = replaceTemplateVar(out, "temp", tempText)
	out = replaceTemplateVar(out, "gamma", gammaText)
	out = replaceTemplateVar(out, "config_temp", strconv.Itoa(configTemp))
	out = replaceTemplateVar(out, "config_gamma", strconv.Itoa(configGamma))
	return out
}

// hyprsunset is the module: the night-light toggle over a spawned
// hyprsunset child.
type hyprsunsetModule struct {
	ctx     ModuleContext
	label   *widget.Label
	icon    widget.Widget
	root    widget.Widget
	child   *exec.Cmd
	enabled bool
	temp    int
	gamma   int
}

func newHyprsunset(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Hyprsunset
	m := &hyprsunsetModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg), temp: cfg.Temperature, gamma: cfg.Gamma}
	m.icon = moduleIcon(ctx, cfg.IconOff)
	if m.icon != nil {
		row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
		row.Append(m.icon, false)
		row.Append(m.label, false)
		m.root = row
	} else {
		m.root = m.label
	}
	m.render()
	return m, nil
}

// render applies the enabled state to label and icon.
func (m *hyprsunsetModule) render() {
	cfg := m.ctx.Config.Hyprsunset
	text := ""
	if cfg.LabelShow {
		text = hyprsunsetLabel(cfg.Format, m.enabled, m.temp, m.gamma, cfg.Temperature, cfg.Gamma)
	}
	m.label.SetText(text)
	if setter, ok := m.icon.(interface {
		SetThemeName(name string)
	}); ok {
		icon := cfg.IconOff
		if m.enabled {
			icon = cfg.IconOn
		}
		setter.SetThemeName(icon.Name)
	}
}

// RunAction handles the module's own `:toggle`; the rest fall through.
func (m *hyprsunsetModule) RunAction(action config.ClickAction) {
	if action.Kind == config.ClickShell && action.Command == ":toggle" {
		m.toggle()
		return
	}
	runClickAction(m.ctx, action)
}

// toggle spawns hyprsunset with the configured temperature and gamma,
// or SIGTERMs the tracked child (its exit handler restores the gamma
// ramp). One global child, like the Rust module's ponytail note.
func (m *hyprsunsetModule) toggle() {
	if m.enabled {
		m.setEnabled(false)
		return
	}
	cfg := m.ctx.Config.Hyprsunset
	child := exec.Command("hyprsunset", //nolint:gosec // the binary name and numeric args are the module's own
		"-t", strconv.Itoa(cfg.Temperature),
		"-g", strconv.Itoa(cfg.Gamma))
	if err := child.Start(); err != nil {
		log.Printf("hyprsunset: start: %v", err)
		return
	}
	m.child = child
	m.temp, m.gamma = cfg.Temperature, cfg.Gamma
	m.setEnabled(true)
	go func() {
		_ = m.child.Wait()
	}()
}

// setEnabled flips the state, terminating any tracked child first.
func (m *hyprsunsetModule) setEnabled(enabled bool) {
	if !enabled && m.child != nil && m.child.Process != nil {
		if err := m.child.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("hyprsunset: stop: %v", err)
		}
		m.child = nil
	}
	m.enabled = enabled
	m.render()
}

func (m *hyprsunsetModule) Root() widget.Widget { return m.root }
