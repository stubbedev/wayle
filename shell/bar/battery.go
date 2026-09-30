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

// thresholdColor returns the label-color override the thresholds
// resolve for value (evaluate_thresholds: last match wins).
func thresholdColor(value float64, thresholds []config.ThresholdEntry, palette *styling.Palette) (render.Color, bool) {
	return resolveOverride(config.EvaluateThresholds(value, thresholds).LabelColor, palette)
}

// resolveOverride resolves an optional color override.
func resolveOverride(cv *config.ColorValue, palette *styling.Palette) (render.Color, bool) {
	if cv == nil {
		return 0, false
	}
	return styling.ResolveColor(*cv, palette)
}

// battery is the module: a label fed from the UPower device snapshot,
// refreshed on PropertiesChanged.
type battery struct {
	buttonRef
	ctx    ModuleContext
	source upower.Source
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
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
	m.icon = moduleIcon(ctx, ctx.Config.Battery.Icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if err := m.subscribe(); err != nil {
		return nil, err
	}
	return m, nil
}

// refresh reads the device and updates the label and state icon.
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
	m.thresholds(dev.Percentage, cfg.Thresholds)
	m.label.SetText(label)
	m.setIcon(cfg, dev)
	return nil
}

// setIcon follows battery helpers.rs's select_icon: alert when absent
// or unknown, charging when charging or pending charge, else the
// level list bucketed by percentage.
func (m *battery) setIcon(cfg config.BatteryConfig, dev upower.Device) {
	setter := m.icon
	if setter == nil {
		return
	}
	var name string
	switch {
	case !dev.Present() || dev.State == upower.StateUnknown:
		name = cfg.AlertIcon
	case dev.State == upower.StateCharging || dev.State == upower.StatePendingCharge:
		name = cfg.ChargingIcon
	case len(cfg.LevelIcons) == 0:
		name = cfg.AlertIcon
	default:
		name = cfg.LevelIcons[levelIndexFloor(dev.Percentage, len(cfg.LevelIcons))]
	}
	setter.SetThemeName(name)
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

func (m *battery) Root() widget.Widget { return m.root }
