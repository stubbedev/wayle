package bar

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

// volumeLabel renders the format; "percent" like the other level
// modules. A muted sink keeps its percent label: mute shows through
// the icon (icon-muted), exactly as methods.rs's update_display.
func volumeLabel(format string, percent float64) string {
	return replaceTemplateVar(format, "percent", strconv.Itoa(int(math.Round(percent))))
}

// volumePercent is the level the Rust modules display: the channel
// average as a percentage, rounded (average_percentage().round()).
func volumePercent(dev pulse.Device) float64 {
	return math.Round(dev.Volume.AveragePercentage())
}

// volumeColor resolves the label ink: the thresholds by the rounded
// level (apply_thresholds), else the default fg. Mute does not recolor
// (methods.rs applies the thresholds on the level alone).
func volumeColor(dev pulse.Device, cfg config.VolumeConfig, palette *styling.Palette, fallback render.Color) render.Color {
	if override, ok := thresholdColor(volumePercent(dev), cfg.Thresholds, palette); ok {
		return override
	}
	return fallback
}

// volume is the module: the default sink's volume label, refreshed on
// PulseAudio events, scroll to adjust.
type volumeModule struct {
	ctx    ModuleContext
	source pulse.Source
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
}

func newVolume(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("volume: requires the application loop")
	}
	if ctx.Pulse == nil {
		return nil, errors.New("volume: no audio source available")
	}
	m := &volumeModule{ctx: ctx, source: ctx.Pulse}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.Volume.Icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Pulse.Subscribe(context.Background())
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

// refresh re-reads and restyles the label and state icon.
func (m *volumeModule) refresh() error {
	out, err := m.source.DefaultSink(context.Background())
	if err != nil {
		return err
	}
	dev := out.Device
	cfg := m.ctx.Config.Volume
	label := ""
	if cfg.LabelShow {
		label = volumeLabel(cfg.Format, volumePercent(dev))
	}
	m.label.SetText(label)
	m.label.SetColor(volumeColor(dev, cfg, m.ctx.Style.palette, m.ctx.Style.fg))
	if setter := m.icon; setter != nil {
		setter.SetThemeName(volumeIconName(cfg, dev))
	}
	return nil
}

// volumeIconName is volume helpers.rs's select_icon: the muted icon
// wins, then the level list spans 1..100 across icons 0..n-1 (0%
// takes the first). The percentage is the rounded average the Rust
// module feeds in.
func volumeIconName(cfg config.VolumeConfig, dev pulse.Device) string {
	if dev.Muted || len(cfg.LevelIcons) == 0 {
		return cfg.IconMuted
	}
	return cfg.LevelIcons[levelIndexSpan(int(volumePercent(dev)), len(cfg.LevelIcons))]
}

func (m *volumeModule) Root() widget.Widget { return m.root }
