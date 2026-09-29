package bar

import (
	"fmt"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// ModuleContext carries what a module needs at construction time: the
// resolved bar style, the config, and the loop it schedules timers on.
// App is nil only in headless construction (tests): the module then
// builds its tree without scheduling updates.
type ModuleContext struct {
	Config *config.Config
	App    *app.Application
	Font   render.Font
	Style  *barStyle
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
	row.Append(module.Root(), false)
	return nil
}
