package bar

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/pulse"
)

// microphoneLabel is helpers.rs's format_label: the rounded percentage. A
// muted mic dims via the color, like the Rust icon-muted semantics.
func microphoneLabel(format string, percent float64) string {
	return replaceTemplateVar(format, "percent", strconv.Itoa(int(math.Round(percent))))
}

// microphone is the module: the default input's level.
type microphoneModule struct {
	ctx    ModuleContext
	source pulse.Source
	label  *widget.Label
	icon   widget.Widget
	root   widget.Widget
}

func newMicrophone(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("microphone: requires the application loop")
	}
	if ctx.Pulse == nil {
		return nil, errors.New("microphone: no audio source available")
	}
	m := &microphoneModule{ctx: ctx, source: ctx.Pulse}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, ctx.Config.Microphone.Icon)
	m.root = assembleModule(ctx, ctx.Config.Microphone.Icon, m.label)
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
func (m *microphoneModule) refresh() error {
	dev, err := m.source.DefaultSource(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Microphone
	label := ""
	if cfg.LabelShow {
		label = microphoneLabel(cfg.Format, volumePercent(dev.Device))
	}
	color := m.ctx.Style.fg
	if dev.Muted {
		color = mutedFg(m.ctx.Style.palette)
	}
	m.label.SetText(label)
	m.label.SetColor(color)
	if setter, ok := m.icon.(interface{ SetThemeName(name string) }); ok {
		name := cfg.Icon.Name
		if dev.Muted {
			name = cfg.IconMuted
		}
		setter.SetThemeName(name)
	}
	return nil
}

func (m *microphoneModule) Root() widget.Widget { return m.root }
