package bar

import (
	"context"
	"errors"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/styling"
)

// microphoneLabel is helpers.rs's format_label: bare percentage. A
// muted mic dims via the color, like the Rust icon-muted semantics.
func microphoneLabel(format string, percent float64) string {
	return replaceTemplateVar(format, "percent", strconv.Itoa(int(percent)))
}

// microphone is the module: the default input's level.
type microphoneModule struct {
	ctx    ModuleContext
	source pulse.Source
	label  *widget.Label
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
func (m *microphoneModule) refresh() error {
	dev, err := m.source.DefaultSource(context.Background())
	if err != nil {
		return err
	}
	cfg := m.ctx.Config.Microphone
	label := ""
	if cfg.LabelShow {
		label = microphoneLabel(cfg.Format, dev.Volume)
	}
	color := m.ctx.Style.fg
	if dev.Muted {
		if muted, ok := styling.ResolveColor(config.ColorValue{Token: config.TokenFgMuted}, m.ctx.Style.palette); ok {
			color = muted
		}
	}
	m.label.SetText(label)
	m.label.SetColor(color)
	return nil
}

func (m *microphoneModule) Root() widget.Widget { return m.label }
