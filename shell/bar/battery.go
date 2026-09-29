package bar

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/upower"
	"github.com/stubbedev/wayle/styling"
)

// batteryLabel renders the format template: the only variable the Rust
// module's context carries is "percent" (rounded), and the braces are
// whitespace-insensitive. No battery maps to the unavailable label.
func batteryLabel(format string, percentage float64, present bool) string {
	if !present {
		return "N/A"
	}
	percent := strconv.Itoa(int(math.Round(percentage)))
	return replaceTemplateVar(format, "percent", percent)
}

// replaceTemplateVar substitutes {{ name }} and {{name}} occurrences.
func replaceTemplateVar(format, name, value string) string {
	for _, pattern := range []string{"{{ " + name + " }}", "{{" + name + "}}"} {
		format = strings.ReplaceAll(format, pattern, value)
	}
	return format
}

// thresholdColor returns the first matching threshold's color override.
func thresholdColor(percentage float64, thresholds []config.ThresholdEntry, palette *styling.Palette) (render.Color, bool) {
	for _, t := range thresholds {
		if t.Matches(percentage) && t.ColorSet {
			if color, ok := styling.ResolveColor(t.IconColor, palette); ok {
				return color, true
			}
		}
	}
	return 0, false
}

// battery is the module: a label fed from the UPower device snapshot,
// refreshed on PropertiesChanged.
type battery struct {
	ctx    ModuleContext
	source upower.Source
	label  *widget.Label
	cancel context.CancelFunc
}

func newBattery(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		// Headless construction has no loop to subscribe on.
		return nil, errors.New("battery: requires the application loop")
	}
	if ctx.Battery == nil {
		return nil, errors.New("battery: no battery source available")
	}
	m := &battery{ctx: ctx, source: ctx.Battery}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if err := m.subscribe(); err != nil {
		return nil, err
	}
	return m, nil
}

// refresh reads the device and updates the label.
func (m *battery) refresh() error {
	dev, err := m.source.Read(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Battery
	label := ""
	if cfg.LabelShow {
		label = batteryLabel(cfg.Format, dev.Percentage, dev.Present())
	}
	color := m.ctx.Style.fg
	if override, ok := thresholdColor(dev.Percentage, cfg.Thresholds, m.ctx.Style.palette); ok {
		color = override
	}
	m.label.SetText(label)
	m.label.SetColor(color)
	return nil
}

// subscribe re-reads on every PropertiesChanged tick.
func (m *battery) subscribe() error {
	ctx, cancel := context.WithCancel(context.Background())
	ticks, stop, err := m.source.Subscribe(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("battery: subscribe: %w", err)
	}
	m.cancel = cancel
	go func() {
		for range ticks {
			m.ctx.Invoke(func() {
				_ = m.refresh()
			})
		}
		stop()
	}()
	return nil
}

func (m *battery) Root() widget.Widget { return m.label }
