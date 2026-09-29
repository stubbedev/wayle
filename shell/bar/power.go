package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"
)

// power is the icon-only power module: its bindings run the session
// commands, with the schema's default left-click being the native
// `:menu` power menu (a dropdown; it logs until the panels land).
type powerModule struct {
	ctx  ModuleContext
	icon widget.Widget
}

func newPower(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Power
	ic := moduleIcon(ctx, cfg.Icon)
	if ic == nil {
		return nil, errors.New("power: icon-show is off and there is nothing to render")
	}
	return &powerModule{ctx: ctx, icon: ic}, nil
}

func (m *powerModule) Root() widget.Widget { return m.icon }
