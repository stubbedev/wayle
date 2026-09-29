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
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/upower"
)

// ModuleContext carries what a module needs at construction time: the
// resolved bar style, the config, and the loop it schedules timers on.
// App is nil only in headless construction (tests): the module then
// builds its tree without scheduling updates. Hyprland is nil when the
// compositor is not Hyprland; Hyprland-only modules error in that case.
type ModuleContext struct {
	Config     *config.Config
	App        *app.Application
	Font       render.Font
	Style      *barStyle
	Hyprland   *hyprland.Connection
	Battery    upower.Source
	Brightness brightness.Source
	Pulse      pulse.Source
	Media      mpris.Source
	Bluetooth  bluetooth.Source
	Network    network.Source
	// Connector is the output this bar instance sits on; per-output
	// modules (workspaces) key their state on it.
	Connector string
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
	"volume":              newVolume,
	"clock":               newClock,
	"cava":                newCava,
	"hyprland-workspaces": newHyprlandWorkspaces,
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

// CreateAll builds every item of one layout section into a row.
// Groups become bar-group containers: a styled box (background,
// padding, and rounding through the .bar-group stylesheet rule)
// holding the group's modules with the group gap.
func CreateAll(items []config.BarItem, ctx ModuleContext) (*widget.Box, error) {
	row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)
	for _, item := range items {
		if item.IsGroup() {
			group := widget.NewBox(widget.Row, ctx.Style.groupGap, 0)
			group.AddClass("bar-group")
			for _, inner := range item.Group.Modules {
				if err := appendModule(group, inner, ctx); err != nil {
					return nil, err
				}
			}
			row.Append(group, false)
			continue
		}
		if err := appendModule(row, item, ctx); err != nil {
			return nil, err
		}
	}
	return row, nil
}

func appendModule(row *widget.Box, item config.BarItem, ctx ModuleContext) error {
	module, err := Create(item.Module, ctx)
	if err != nil {
		return err
	}
	binding := moduleBinding(item.Module, ctx.Config)
	row.Append(wrapActions(module.Root(), binding, func(action config.ClickAction) {
		runClickAction(ctx, action)
	}), false)
	return nil
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
