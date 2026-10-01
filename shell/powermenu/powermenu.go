// Package powermenu is the native power menu (shell/power_menu): a
// full-screen dimmed overlay with a centered row of lock, log out,
// suspend, reboot and shut-down buttons, each running its
// [modules.power] command. The power module's `:menu` binding opens
// it; Escape dismisses it. It enters and leaves through the power
// surface's transition, and a button's command runs once the menu is
// gone.
package powermenu

import (
	"log"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/shell/reveal"
)

// iconPx is the button icon size (--icon-lg).
const iconPx = 32

// escape dismisses the menu.
var escape, _ = app.ParseAccel("Escape")

// action is one button: its icon, label, command and visibility.
type action struct {
	icon, label string
	command     func(config.PowerConfig) string
	visible     func(config.PowerConfig) bool
}

// actions are ACTIONS, in their order.
var actions = []action{
	{"ld-lock-symbolic", "Lock", func(c config.PowerConfig) string { return c.LockCommand }, func(c config.PowerConfig) bool { return c.ShowLock }},
	{"ld-log-out-symbolic", "Log out", func(c config.PowerConfig) string { return c.LogoutCommand }, func(c config.PowerConfig) bool { return c.ShowLogout }},
	{"ld-moon-symbolic", "Suspend", func(c config.PowerConfig) string { return c.SuspendCommand }, func(c config.PowerConfig) bool { return c.ShowSuspend }},
	{"ld-rotate-ccw-symbolic", "Reboot", func(c config.PowerConfig) string { return c.RebootCommand }, func(c config.PowerConfig) bool { return c.ShowReboot }},
	{"ld-power-symbolic", "Shut down", func(c config.PowerConfig) string { return c.ShutdownCommand }, func(c config.PowerConfig) bool { return c.ShowShutdown }},
}

// Window is the overlay surface the menu maps (app.LayerWindow).
type Window interface{ Close() }

// Deps are the menu's collaborators.
type Deps struct {
	// Config is the live config; commands are read on click.
	Config func() *config.Config
	// Open maps the overlay.
	Open func(cfg app.LayerConfig) (Window, error)
	// Run runs a command (spawn.Quiet).
	Run func(cmd string) error
	// Font labels the buttons in Ink until Sheet (the power-menu-*
	// classes) restyles them.
	Font  render.Font
	Ink   render.Color
	Sheet *widget.Stylesheet
}

// Menu is the power menu. Its methods run on the UI loop.
type Menu struct {
	d   Deps
	win Window
	rev *widget.Revealer
	// closing: the exit is playing; a Show reopens a fresh surface.
	closing bool
}

// New builds the menu; nothing maps until Show.
func New(d Deps) *Menu { return &Menu{d: d} }

// Show opens the menu with the current config's buttons, or does
// nothing while it is already up.
func (m *Menu) Show() {
	if m.win != nil && !m.closing {
		return
	}
	cfg := m.d.Config()
	row := widget.NewBox(widget.Row, 12, 0)
	row.AddClass("power-menu-surface")
	for _, a := range actions {
		if !a.visible(cfg.Power) {
			continue
		}
		row.Append(m.button(a), true)
	}
	m.rev = widget.NewRevealer(credential.Center(row))
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass("power-menu-window")
	if m.d.Sheet != nil {
		root.AttachStylesheet(m.d.Sheet)
	}
	root.Append(m.rev, true)
	win, err := m.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardExclusive,
		Namespace:     "wayle-power-menu",
		Root:          root,
		KeyCapture: func(a app.Accel) bool {
			if a.Sym == escape.Sym { // whatever modifiers are held
				m.Cancel()
				return true
			}
			return false
		},
	})
	if err != nil {
		log.Printf("power menu: cannot map the overlay: %v", err)
		return
	}
	m.win, m.closing = win, false
	reveal.Show(m.rev, cfg.Animations, config.AnimPower)
}

// button is one action: icon over label, its command re-read on click.
func (m *Menu) button(a action) widget.Widget {
	icon := widget.NewThemeIcon(a.icon, iconPx)
	icon.AddClass("power-menu-icon")
	label := widget.NewLabel(m.d.Font, 13, a.label, m.d.Ink)
	label.AddClass("power-menu-label")
	label.SetAlignment(render.AlignCenter)
	content := widget.NewBox(widget.Column, 6, 0)
	content.Append(icon, false)
	content.Append(label, false)
	b := widget.NewButton(credential.Center(content), 0, 0)
	b.AddClass("power-menu-button")
	b.OnClick = func() { m.Run(a.command(m.d.Config().Power)) }
	return b
}

// Run plays the exit, closes the menu, then runs cmd (an empty one
// runs nothing).
func (m *Menu) Run(cmd string) { m.hide(cmd) }

// Cancel plays the exit and closes the menu.
func (m *Menu) Cancel() { m.hide("") }

func (m *Menu) hide(then string) {
	if m.win == nil || m.closing {
		return
	}
	m.closing = true
	win := m.win
	reveal.Hide(m.rev, m.d.Config().Animations, config.AnimPower, func() {
		win.Close()
		if m.win == win {
			m.win, m.rev, m.closing = nil, nil, false
		}
		if then != "" {
			_ = m.d.Run(then)
		}
	})
}

// Open reports whether the menu is up (its exit not begun).
func (m *Menu) Open() bool { return m.win != nil && !m.closing }
