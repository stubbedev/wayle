package bar

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/brightness"
)

// brightnessLabel renders the module format; the only variable is
// "percent", the battery module's template semantics.
func brightnessLabel(format string, percent float64, present bool) string {
	if !present {
		return "N/A"
	}
	return replaceTemplateVar(format, "percent", strconv.Itoa(int(math.Round(percent))))
}

// averagePercentage is helpers.rs's average across devices; nil when
// no device reports (absent battery-style N/A).
func averagePercentage(devices []brightness.Device) (float64, bool) {
	if len(devices) == 0 {
		return 0, false
	}
	var sum float64
	for _, d := range devices {
		sum += d.Percentage()
	}
	return sum / float64(len(devices)), true
}

// brightness is the module: the mean percentage label across backlights,
// refreshed on sysfs changes.
type brightnessModule struct {
	buttonRef
	ctx    ModuleContext
	source brightness.Source
	label  *widget.Label
}

func newBrightness(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("brightness: requires the application loop")
	}
	if ctx.Brightness == nil {
		return nil, errors.New("brightness: no backlight source available")
	}
	m := &brightnessModule{ctx: ctx, source: ctx.Brightness}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Brightness.Subscribe(ctx.Life())
	if err != nil {
		return nil, err
	}
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
		stop()
	}()
	return m, nil
}

// refresh re-reads and restyles the label.
func (m *brightnessModule) refresh() error {
	devices, err := m.source.Devices(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Brightness
	label := ""
	if percent, ok := averagePercentage(devices); ok {
		if cfg.LabelShow {
			label = brightnessLabel(cfg.Format, percent, true)
		}
		m.thresholds(percent, cfg.Thresholds)
	} else {
		if cfg.LabelShow {
			label = brightnessLabel(cfg.Format, 0, false)
		}
		m.thresholds(0, nil)
	}
	m.label.SetText(label)
	return nil
}

func (m *brightnessModule) Root() widget.Widget { return m.label }
