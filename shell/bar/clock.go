package bar

import (
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/strftime"
)

// labelPx is the bar's base font size in logical pixels: the default
// button-label-size until the styling port resolves it from config.
const labelPx = 14

// clock renders the time through the configured strftime format and
// keeps itself current once a second.
type clock struct {
	label *widget.Label
}

func newClock(ctx ModuleContext) (Module, error) {
	layout, err := strftime.Compile(ctx.Config.Clock.Format)
	if err != nil {
		return nil, err
	}
	label := widget.NewLabel(ctx.Font, labelPx, layout.Format(time.Now()), ctx.Theme.Text)
	ctx.App.Every(time.Second, func() {
		label.SetText(layout.Format(time.Now()))
	})
	return &clock{label: label}, nil
}

func (c *clock) Root() widget.Widget { return c.label }
