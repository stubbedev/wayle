package bar

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
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
	"github.com/stubbedev/wayle/service/weather"
	"github.com/stubbedev/wayle/shell/apptheme"
	"github.com/stubbedev/wayle/shell/powermenu"
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
	Weather       *weather.Service
	// SetConfig writes a runtime override and persists it (the Rust
	// ConfigService property set the dropdowns use); nil without a
	// config service.
	SetConfig func(path string, value any)
	// Toast shows an OSD toast (the Rust ToastBus); nil without an OSD.
	Toast func(label, icon string)
	// Clipboard is the session clipboard history, nil when the
	// compositor has no data-control protocol (the launcher's
	// clipboard mode reads it).
	Clipboard *clipboard.Clipboard
	// Tray drives the tray items and their menus; nil without a host.
	Tray          TrayService
	CustomUpdates *customUpdates
	// PowerMenu is the native power menu the power module's :menu
	// opens; nil without one.
	PowerMenu *powermenu.Menu
	// Dropdowns opens the dropdown:<name> popovers; RunWith owns one
	// registry across outputs.
	Dropdowns *dropdownRegistry
	// Attachers collects modules that need the live layer surface
	// (idle-inhibit binds its inhibitor to it); RunWith calls Attach
	// on each once the layer exists.
	Attachers *[]interface{ Attach(app.Host) }
	// gen is the mount generation the module belongs to; a config
	// reload retires it, so the old modules' scheduled work stops.
	gen *mountGen
	// Connector is the output this bar instance sits on; per-output
	// modules (workspaces) key their state on it.
	Connector string
	// Screenshot starts a capture on the in-process screenshot host
	// (the `wayle screenshot ...` builtin); nil before the host is up.
	Screenshot func(mode, target string)
	// Theme is the bar stylesheet every bar root attaches; nil in
	// headless construction, where the tree builds unstyled.
	Theme *apptheme.Theme
}

// headlessLoop stands in for the loop goroutine of a headless context
// (tests): work Invoke would marshal runs inline under it, so widget
// state is touched by one goroutine at a time either way.
var headlessLoop sync.Mutex

// Invoke marshals fn onto the loop goroutine, or for a headless context
// runs it inline under headlessLoop. A retired generation's work is
// dropped either way.
func (c ModuleContext) Invoke(fn func()) {
	if c.App == nil {
		if c.gen.retiredNow() {
			return
		}
		headlessLoop.Lock()
		defer headlessLoop.Unlock()
		fn()
		return
	}
	c.App.Invoke(func() {
		if !c.gen.retiredNow() {
			fn()
		}
	})
}

// Every schedules fn on the application loop; a no-op when the context
// is headless.
func (c ModuleContext) Every(d time.Duration, fn func()) {
	if c.App == nil {
		return
	}
	c.App.Every(d, func() {
		if !c.gen.retiredNow() {
			fn()
		}
	})
}

// mountGen is one generation of mounted bars. A config reload retires
// it before mounting the next: the old modules' Invoke and Every work
// stops touching their (closed) trees, and their followed
// subscriptions end.
type mountGen struct {
	retired atomic.Bool
	once    sync.Once
	done    chan struct{}
	life    context.Context
	cancel  context.CancelFunc
}

func newMountGen() *mountGen {
	life, cancel := context.WithCancel(context.Background())
	return &mountGen{done: make(chan struct{}), life: life, cancel: cancel}
}

// Life is the module's lifetime: canceled when its generation
// retires (a config reload), never for a context outside one. Module
// loops and subscriptions run under it.
func (c ModuleContext) Life() context.Context {
	if c.gen == nil {
		return context.Background()
	}
	return c.gen.life
}

func (g *mountGen) retiredNow() bool { return g != nil && g.retired.Load() }

// retire ends the generation.
func (g *mountGen) retire() {
	g.once.Do(func() {
		g.retired.Store(true)
		close(g.done)
		g.cancel()
	})
}

// ended is closed once the generation retires; nil (never) for a
// context outside any generation.
func (g *mountGen) ended() <-chan struct{} {
	if g == nil {
		return nil
	}
	return g.done
}

// follow runs fn for every value from ch until the module's
// generation retires or ch closes, then stops the subscription. fn
// runs through Invoke.
func follow[T any](ctx ModuleContext, ch <-chan T, stop func(), fn func(T)) {
	go func() {
		if stop != nil {
			defer stop()
		}
		for {
			select {
			case <-ctx.gen.ended():
				return
			case v, ok := <-ch:
				if !ok {
					return
				}
				ctx.Invoke(func() { fn(v) })
			}
		}
	}()
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
		return newCustomByID(ctx, def.Id)
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
//
// A module that cannot be built (its service is unavailable on this
// system, its compositor is not running) is logged and left out, like
// module_registry.rs require_service: one missing daemon never takes
// the bar down.
func CreateAll(items []config.BarItem, ctx ModuleContext) *widget.Box {
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
				log.Printf("bar: module %q will not appear: %v", inner.Module, err)
			}
		}
		row.Append(box, false)
	}
	return row
}

func appendModule(row *widget.Box, item config.BarItem, ctx ModuleContext) error {
	module, err := Create(string(item.Module), ctx)
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
	follower, _ := module.(interface {
		followAction(action config.ClickAction, scroll bool)
	})
	btn.configure(moduleButton(string(item.Module), ctx.Config), moduleBinding(string(item.Module), ctx.Config), func(action config.ClickAction, scroll bool) {
		switch {
		case action.Kind == config.ClickDropdown && ctx.Dropdowns != nil:
			// Dropdown bindings anchor to this module's own button; the
			// registry toggles the popover on the connector's host.
			_ = ctx.Dropdowns.open(ctx.Connector, action.Dropdown, btn)
		case handler != nil:
			handler.RunAction(action)
		default:
			runClickAction(ctx, action)
		}
		// A module that reacts after the binding (the custom module's
		// on-action) hears every one.
		if follower != nil {
			follower.followAction(action, scroll)
		}
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

// moduleButton is the bar-button view a layout item's module carries;
// a module without the key set (or no config) gets the neutral set.
func moduleButton(name string, cfg *config.Config) config.ButtonConfig {
	if cfg != nil {
		if b, ok := cfg.ModuleButton(name); ok {
			return b
		}
		if def, ok := customDefinition(name, cfg); ok {
			return def.Button()
		}
	}
	return config.ButtonConfig{IconShow: true, LabelShow: true}
}

// moduleBinding is the module's click and scroll bindings.
func moduleBinding(name string, cfg *config.Config) config.ClickConfig {
	if clicks, ok := cfg.ModuleClicks(name); ok {
		return clicks
	}
	if def, ok := customDefinition(name, cfg); ok {
		return def.Clicks()
	}
	return config.ClickConfig{}
}
