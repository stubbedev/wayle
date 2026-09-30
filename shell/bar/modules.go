package bar

import (
	"fmt"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/bluetooth"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/service/clipboard"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/service/mail"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/sni"
	"github.com/stubbedev/wayle/service/treeman"
	"github.com/stubbedev/wayle/service/upower"
)

// ModuleContext carries what a module needs at construction time: the
// resolved bar style, the config, and the loop it schedules timers on.
// App is nil only in headless construction (tests): the module then
// builds its tree without scheduling updates. Hyprland is nil when the
// compositor is not Hyprland; Hyprland-only modules error in that case.
type ModuleContext struct {
	Config        *config.Config
	App           *app.Application
	Font          render.Font
	Style         *barStyle
	Hyprland      *hyprland.Connection
	Battery       upower.Source
	Brightness    brightness.Source
	Pulse         pulse.Source
	Media         mpris.Source
	Bluetooth     bluetooth.Source
	Network       network.Source
	Networking    *network.Service // secret agent, VPNs, wifi; nil without NM
	PowerProfiles powerprofiles.Source
	IdleInhibit   *idleinhibit.State
	Treeman       treeman.Source
	Notifications *notifications.Service
	Recorder      *recorder.State
	Mail          *mail.Service
	SNI           *sni.Store
	// Clipboard is the session clipboard history, nil when the
	// compositor has no data-control protocol (the launcher's
	// clipboard mode reads it).
	Clipboard *clipboard.Clipboard
	// Tray drives the tray items and their menus; nil without a host.
	Tray          TrayService
	CustomUpdates *customUpdates
	// Dropdowns opens the dropdown:<name> popovers; RunWith owns one
	// registry across outputs.
	Dropdowns *dropdownRegistry
	// Attachers collects modules that need the live layer surface
	// (idle-inhibit binds its inhibitor to it); RunWith calls Attach
	// on each once the layer exists.
	Attachers *[]interface{ Attach(app.Host) }
	// Connector is the output this bar instance sits on; per-output
	// modules (workspaces) key their state on it.
	Connector string
	// Screenshot starts a capture on the in-process screenshot host
	// (the `wayle screenshot ...` builtin); nil before the host is up.
	Screenshot func(mode, target string)
	// Theme is the bar stylesheet every bar root attaches; nil in
	// headless construction, where the tree builds unstyled.
	Theme *barTheme
}

// Invoke marshals fn onto the loop goroutine; a no-op when the context
// is headless.
func (c ModuleContext) Invoke(fn func()) {
	if c.App == nil {
		return
	}
	c.App.Invoke(fn)
}

// Every schedules fn on the application loop; a no-op when the context
// is headless.
func (c ModuleContext) Every(d time.Duration, fn func()) {
	if c.App == nil {
		return
	}
	c.App.Every(d, fn)
}

// Module is one bar module: a live widget tree plus whatever timers
// keep it current. The interface grows update/message plumbing as
// services land.
type Module interface {
	Root() widget.Widget
}

// Factory builds one module instance.
type Factory func(ctx ModuleContext) (Module, error)

var factories = map[string]Factory{
	"battery":             newBattery,
	"brightness":          newBrightness,
	"keyboard-input":      newKeyboardLayout,
	"media":               newMedia,
	"bluetooth":           newBluetooth,
	"microphone":          newMicrophone,
	"network":             newNetwork,
	"window-title":        newWindowTitle,
	"cpu":                 newCpu,
	"ram":                 newRam,
	"storage":             newStorage,
	"weather":             newWeather,
	"world-clock":         newWorldClock,
	"netstat":             newNetstat,
	"mail":                newMail,
	"power":               newPower,
	"dashboard":           newDashboard,
	"keybind-mode":        newKeybindMode,
	"power-profiles":      newPowerProfiles,
	"hyprsunset":          newHyprsunset,
	"idle-inhibit":        newIdleInhibit,
	"treeman":             newTreeman,
	"notifications":       newNotification,
	"recorder":            newRecorder,
	"systray":             newSystray,
	"volume":              newVolume,
	"clock":               newClock,
	"cava":                newCava,
	"hyprland-workspaces": newHyprlandWorkspaces,
	"sway-workspaces":     newSwayWorkspaces,
	"niri-workspaces":     newNiriWorkspaces,
	"mango-workspaces":    newMangoWorkspaces,
	"screenshot":          newScreenshot,
	"separator":           newSeparator,
}

// Create builds the module named by a layout entry. Custom modules
// (custom-*) resolve through the [modules.custom] definitions; names
// outside both are errors: a layout naming a module the Go shell
// cannot build fails loudly instead of silently shortening the bar.
func Create(name string, ctx ModuleContext) (Module, error) {
	if def, ok := customDefinition(name, ctx.Config); ok {
		return newCustomByID(ctx, def.ID)
	}
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("bar: module %q not ported to the Go shell yet", name)
	}
	return factory(ctx)
}

// CreateAll builds every item of one layout section into a row: one
// box.bar-item per layout item (bar/factory.rs BarItemFactory), a
// group's carrying the bar-group class and the group name as its id,
// each module inside classed `module` plus its per-instance class. The
// gaps between items and grouped modules are the stylesheet's margins
// (bar/_layout.scss), not box spacing.
func CreateAll(items []config.BarItem, ctx ModuleContext) (*widget.Box, error) {
	axis := widget.Row
	if ctx.Config != nil && ctx.Config.Bar.Location.IsVertical() {
		axis = widget.Column
	}
	row := widget.NewBox(axis, 0, 0)
	for _, item := range items {
		box := widget.NewBox(axis, 0, 0)
		box.AddClass("bar-item")
		modules := []config.BarItem{item}
		if item.IsGroup() {
			box.AddClass("bar-group")
			box.SetID(item.Group.Name)
			modules = item.Group.Modules
		}
		for _, inner := range modules {
			if err := appendModule(box, inner, ctx); err != nil {
				return nil, err
			}
		}
		row.Append(box, false)
	}
	return row, nil
}

func appendModule(row *widget.Box, item config.BarItem, ctx ModuleContext) error {
	module, err := Create(item.Module, ctx)
	if err != nil {
		return err
	}
	if a, ok := module.(interface{ Attach(app.Host) }); ok && ctx.Attachers != nil {
		*ctx.Attachers = append(*ctx.Attachers, a)
	}
	root := module.Root()
	classes := []string{"module"}
	if item.Class != "" {
		classes = append(classes, item.Class)
	}
	// Components that are not BarButtons in the Rust shell (the
	// workspace rows) carry their own chrome and input handling.
	if _, ok := module.(interface{ ownsChrome() }); ok {
		if n, ok := root.(interface{ AddClass(...string) }); ok {
			n.AddClass(classes...)
		}
		row.Append(root, false)
		return nil
	}
	var handler interface {
		RunAction(config.ClickAction)
	}
	if h, ok := module.(interface {
		RunAction(config.ClickAction)
	}); ok {
		handler = h
	}
	btn := asBarButton(ctx, root)
	btn.configure(moduleButton(item.Module, ctx.Config), moduleBinding(item.Module, ctx.Config), func(action config.ClickAction) {
		// Dropdown bindings anchor to this module's own button; the
		// registry toggles the popover on the connector's host.
		if action.Kind == config.ClickDropdown && ctx.Dropdowns != nil {
			_ = ctx.Dropdowns.open(ctx.Connector, action.Dropdown, btn)
			return
		}
		if handler != nil {
			handler.RunAction(action)
			return
		}
		runClickAction(ctx, action)
	})
	btn.AddClass(classes...)
	if r, ok := module.(interface{ setButton(*barButton) }); ok {
		r.setButton(btn)
	}
	row.Append(btn, false)
	return nil
}

// asBarButton returns the module root as a bar button: the one the
// module assembled, a bare label given the label container, or any
// other content wrapped in the button chrome.
func asBarButton(ctx ModuleContext, root widget.Widget) *barButton {
	switch r := root.(type) {
	case *barButton:
		return r
	case *widget.Label:
		return newBarButton(ctx, nil, r)
	}
	return newBarButtonAround(ctx, root)
}

// moduleButton looks up the bar-button keys a layout item's module
// carries; modules without them (custom modules resolve their own) get
// the schema's neutral set.
func moduleButton(name string, cfg *config.Config) config.ButtonConfig {
	if cfg == nil {
		return config.ButtonConfig{IconShow: true, LabelShow: true}
	}
	switch name {
	case "battery":
		return cfg.Battery.Button
	case "brightness":
		return cfg.Brightness.Button
	case "volume":
		return cfg.Volume.Button
	case "media":
		return cfg.Media.Button
	case "network":
		return cfg.Network.Button
	case "bluetooth":
		return cfg.Bluetooth.Button
	case "microphone":
		return cfg.Microphone.Button
	case "keyboard-input":
		return cfg.KeyboardInput.Button
	case "window-title":
		return cfg.WindowTitle.Button
	case "cpu":
		return cfg.CPU.Button
	case "ram":
		return cfg.RAM.Button
	case "storage":
		return cfg.Storage.Button
	case "weather":
		return cfg.Weather.Button
	case "world-clock":
		return cfg.WorldClock.Button
	case "netstat":
		return cfg.Netstat.Button
	case "mail":
		return cfg.Mail.Button
	case "power":
		return cfg.Power.Button
	case "keybind-mode":
		return cfg.KeybindMode.Button
	case "power-profiles":
		return cfg.PowerProfiles.Button
	case "hyprsunset":
		return cfg.Hyprsunset.Button
	case "idle-inhibit":
		return cfg.IdleInhibit.Button
	case "treeman":
		return cfg.Treeman.Button
	case "notifications":
		return cfg.Notification.Button
	case "recorder":
		return cfg.Recorder.Button
	case "clock":
		return cfg.Clock.Button
	}
	if def, ok := customDefinition(name, cfg); ok {
		return def.Button
	}
	return config.ButtonConfig{IconShow: true, LabelShow: true}
}

// moduleBinding looks up the [modules.<name>] bindings a layout item's
// module carries; unknown modules have none.
func moduleBinding(name string, cfg *config.Config) config.ClickConfig {
	switch name {
	case "battery":
		return cfg.Battery.Click
	case "brightness":
		return cfg.Brightness.Click
	case "volume":
		return cfg.Volume.Click
	case "media":
		return cfg.Media.Click
	case "network":
		return cfg.Network.Click
	case "bluetooth":
		return cfg.Bluetooth.Click
	case "microphone":
		return cfg.Microphone.Click
	case "keyboard-input":
		return cfg.KeyboardInput.Click
	case "window-title":
		return cfg.WindowTitle.Click
	case "cpu":
		return cfg.CPU.Click
	case "ram":
		return cfg.RAM.Click
	case "storage":
		return cfg.Storage.Click
	case "weather":
		return cfg.Weather.Click
	case "world-clock":
		return cfg.WorldClock.Click
	case "netstat":
		return cfg.Netstat.Click
	case "mail":
		return cfg.Mail.Click
	case "screenshot":
		return cfg.Screenshot.Click
	case "power":
		return cfg.Power.Click
	case "dashboard":
		return cfg.Dashboard.Click
	case "keybind-mode":
		return cfg.KeybindMode.Click
	case "power-profiles":
		return cfg.PowerProfiles.Click
	case "hyprsunset":
		return cfg.Hyprsunset.Click
	case "idle-inhibit":
		return cfg.IdleInhibit.Click
	case "treeman":
		return cfg.Treeman.Click
	case "notifications":
		return cfg.Notification.Click
	case "recorder":
		return cfg.Recorder.Click
	case "clock":
		return cfg.Clock.Click
	case "cava":
		return cfg.Cava.Click
	}
	if def, ok := customDefinition(name, cfg); ok {
		return def.Click
	}
	return config.ClickConfig{}
}
