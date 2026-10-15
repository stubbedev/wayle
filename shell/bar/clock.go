package bar

import (
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/strftime"
)

// clock renders the time through the configured strftime format and
// keeps itself current once a second, beside the clock icon.
type clock struct {
	root widget.Widget
}

func newClock(ctx ModuleContext) (Module, error) {
	layout, err := strftime.Compile(ctx.Config.Clock.Format)
	if err != nil {
		return nil, err
	}
	label := widget.NewLabel(ctx.Font, ctx.Style.labelPx, layout.Format(time.Now()), ctx.Style.fg)
	ctx.Every(time.Second, func() {
		label.SetText(layout.Format(time.Now()))
	})
	return &clock{root: assembleModule(ctx, moduleIcon(ctx, ctx.Config.Clock.Icon()), label)}, nil
}

func (c *clock) Root() widget.Widget { return c.root }
