package bar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/strftime"
)

// worldClockRender is helpers.rs's format_world_clock: the format
// renders as a template with tz(zone, strftime) formatting now in that
// zone. An unknown zone fails the render, which shows as nothing
// (format_world_clock(...).unwrap_or_default()).
func worldClockRender(format string, now time.Time) string {
	var env jinja.Env
	env.AddFunction("tz", func(args []any, _ map[string]any) (any, error) {
		if len(args) != 2 {
			return nil, errors.New("tz takes a timezone and a format")
		}
		zone, _ := args[0].(string)
		layout, _ := args[1].(string)
		location, err := time.LoadLocation(zone)
		if err != nil || zone == "" {
			log.Printf("world-clock: invalid timezone identifier %q", zone)
			return nil, fmt.Errorf("invalid timezone: %s", zone)
		}
		formatted, err := strftime.Compile(layout)
		if err != nil {
			return "", nil
		}
		return formatted.Format(now.In(location)), nil
	})
	out, err := env.Render(format, nil)
	if err != nil {
		return ""
	}
	return out
}

// worldClock is the module: the format re-renders every second.
type worldClockModule struct {
	ctx   ModuleContext
	label *widget.Label
	stop  func()
}

func newWorldClock(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("world-clock: requires the application loop")
	}
	cfg := ctx.Config.WorldClock
	m := &worldClockModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.apply(cfg.Format)
	runCtx, cancel := context.WithCancel(ctx.Life())
	m.stop = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			m.ctx.Invoke(func() { m.apply(m.ctx.Config.WorldClock.Format) })
		}
	}()
	return m, nil
}

// apply renders the current format; an unchanged label is a no-op for
// the damage collector.
func (m *worldClockModule) apply(format string) {
	text := ""
	if m.ctx.Config.WorldClock.LabelShow {
		text = worldClockRender(format, time.Now())
	}
	m.label.SetText(text)
}

func (m *worldClockModule) Root() widget.Widget {
	return assembleModule(m.ctx, moduleIcon(m.ctx, m.ctx.Config.WorldClock.Icon()), m.label)
}

// Stop ends the render ticker.
func (m *worldClockModule) Stop() { m.stop() }
