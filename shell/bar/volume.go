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
// modules. A muted sink shows the same label (the Rust module's
// icon-muted governs the icon, which is not ported here).
func volumeLabel(format string, percent float64) string {
	return replaceTemplateVar(format, "percent", strconv.Itoa(int(math.Round(percent))))
}

// volumeColor resolves the label ink: muted wins (the Rust module's
// icon-muted semantics on a label-only surface), then thresholds by
// level, else the default fg.
func volumeColor(dev pulse.Device, cfg config.VolumeConfig, palette *styling.Palette, fallback render.Color) render.Color {
	if dev.Muted {
		if color, ok := styling.ResolveColor(config.ColorValue{Token: config.TokenFgMuted}, palette); ok {
			return color
		}
	}
	if override, ok := thresholdColor(dev.Volume, cfg.Thresholds, palette); ok {
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

// refresh re-reads and restyles the label.
func (m *volumeModule) refresh() error {
	dev, err := m.source.DefaultSink(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Volume
	label := ""
	if cfg.LabelShow {
		label = volumeLabel(cfg.Format, dev.Volume)
	}
	m.label.SetText(label)
	m.label.SetColor(volumeColor(dev, cfg, m.ctx.Style.palette, m.ctx.Style.fg))
	return nil
}

func (m *volumeModule) Root() widget.Widget { return m.label }
