package bar

import (
	"context"
	"errors"
	"math"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/brightness"
)

// brightnessNoDevice is NO_DEVICE_LABEL.
const brightnessNoDevice = "--%"

// brightnessLabel is helpers.rs's format_label: the one variable is
// the rounded percent.
func brightnessLabel(format string, percent float64) string {
	return jinja.RenderOr(format, map[string]any{"percent": int64(math.Round(percent))})
}

// brightnessIcon is select_icon: level-icons split 0-100% into equal
// bands, the last one closing at 100%; no icons is "".
func brightnessIcon(levels []string, percent float64) string {
	if len(levels) == 0 {
		return ""
	}
	i := int(math.Floor(percent / 100 * float64(len(levels))))
	return levels[max(0, min(i, len(levels)-1))]
}

// averagePercentage is helpers.rs's average across devices; false when
// there is none.
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

// brightnessModule is the bar button: the mean percentage across
// backlights with its level icon, refreshed on sysfs changes.
type brightnessModule struct {
	buttonRef
	ctx    ModuleContext
	source brightness.Source
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
}

func newBrightness(ctx ModuleContext) (Module, error) {
	if ctx.Brightness == nil {
		return nil, errors.New("brightness: no backlight source available")
	}
	cfg := ctx.Config.Brightness
	m := &brightnessModule{ctx: ctx, source: ctx.Brightness, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, config.IconWith(cfg.IconShow, brightnessIcon(cfg.LevelIcons, 0), cfg.IconColor))
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if ctx.App == nil {
		return m, nil
	}
	ticks, stop, err := ctx.Brightness.Subscribe(ctx.Life())
	if err != nil {
		return nil, err
	}
	follow(ctx, ticks, stop, func(struct{}) { _ = m.refresh() })
	return m, nil
}

// refresh is refresh_display: the label and icon for the mean level
// and its threshold colors, or "--%" and the first icon without a
// device.
func (m *brightnessModule) refresh() error {
	devices, err := m.source.Devices(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Brightness
	percent, ok := averagePercentage(devices)
	if !ok {
		m.label.SetText(brightnessNoDevice)
		m.setIcon(brightnessIcon(cfg.LevelIcons, 0))
		return nil
	}
	m.label.SetText(brightnessLabel(cfg.Format, percent))
	m.setIcon(brightnessIcon(cfg.LevelIcons, percent))
	m.thresholds(percent, cfg.Thresholds)
	return nil
}

func (m *brightnessModule) setIcon(name string) {
	if m.icon != nil && name != "" {
		m.icon.SetThemeName(name)
	}
}

func (m *brightnessModule) Root() widget.Widget { return m.root }
