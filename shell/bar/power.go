package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// power is the icon-only power module: its bindings run the session
// commands, and the native `:menu` binding opens the power menu.
type powerModule struct {
	ctx  ModuleContext
	icon *widget.Icon
}

func newPower(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Power
	ic := moduleIcon(ctx, cfg.Icon())
	if ic == nil {
		return nil, errors.New("power: icon-show is off and there is nothing to render")
	}
	return &powerModule{ctx: ctx, icon: ic}, nil
}

func (m *powerModule) Root() widget.Widget { return m.icon }

// RunAction intercepts the native :menu verb: it opens the power menu
// in-process (show_power_menu).
func (m *powerModule) RunAction(action config.ClickAction) {
	if action.Kind == config.ClickShell && action.Command == ":menu" {
		if m.ctx.PowerMenu != nil {
			m.ctx.PowerMenu.Show()
		}
		return
	}
	runClickAction(m.ctx, action)
}
