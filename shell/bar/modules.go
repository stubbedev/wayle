package bar

import (
	"fmt"
	"math"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// ModuleContext carries what a module needs at construction time. It
// grows as services (hyprland, audio, ...) are ported.
type ModuleContext struct {
	Config *config.Config
	App    *app.Application
	Font   render.Font
	Theme  *widget.Theme
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
	"clock": newClock,
}

// Create builds the module named by a layout entry. Custom modules
// (custom-*) and names outside the registry are errors: a layout
// naming a module the Go shell cannot build fails loudly instead of
// silently shortening the bar.
func Create(name string, ctx ModuleContext) (Module, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("bar: module %q not ported to the Go shell yet", name)
	}
	return factory(ctx)
}

// CreateAll builds every item of one layout section into a row box.
// Groups flatten into the section row for now: the shared visual
// container lands with the styling port.
func CreateAll(items []config.BarItem, ctx ModuleContext) (*widget.Box, error) {
	row := widget.NewBox(widget.Row, px(ctx.Config.Bar.ModuleGap), 0)
	for _, item := range items {
		if item.IsGroup() {
			for _, inner := range item.Group.Modules {
				if err := appendModule(row, inner, ctx); err != nil {
					return nil, err
				}
			}
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
	row.Append(module.Root(), false)
	return nil
}

// px resolves a config size against the bar's base font size.
func px(s config.Size) int {
	return int(math.Round(s.Px(labelPx)))
}
