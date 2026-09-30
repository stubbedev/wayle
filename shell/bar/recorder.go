package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/styling"
)

// recorderLabel is helpers.rs's build_label: Idle/Paused/Recording
// for {{ state }}, the H:MM:SS (or M:SS) clock for {{ elapsed }}, a
// dash while idle.
func recorderLabel(format string, active, paused bool, elapsedSecs uint32) string {
	state := "Idle"
	switch {
	case active && paused:
		state = "Paused"
	case active:
		state = "Recording"
	}
	elapsed := "-"
	if active {
		elapsed = recorder.FormatElapsed(elapsedSecs)
	}
	out := replaceTemplateVar(format, "state", state)
	return replaceTemplateVar(out, "elapsed", elapsed)
}

// recorderIconName is helpers.rs's select_icon over the state trio.
func recorderIconName(cfg config.RecorderConfig, snap recorder.Change) string {
	if snap.Active || snap.Preparing {
		if snap.Paused {
			return cfg.Icons[config.RecorderPaused].Name
		}
		return cfg.Icons[config.RecorderRecording].Name
	}
	return cfg.Icons[config.RecorderIdle].Name
}

// recorderColor resolves the state's configured color, the bar fg
// otherwise.
func recorderColor(cfg config.RecorderConfig, snap recorder.Change, palette *styling.Palette, fallback render.Color) render.Color {
	name := config.RecorderIdle
	switch {
	case snap.Active && snap.Paused:
		name = config.RecorderPaused
	case snap.Active:
		name = config.RecorderRecording
	}
	color, ok := cfg.Colors[name]
	if !ok {
		return fallback
	}
	if resolved, ok := styling.ResolveColor(color, palette); ok {
		return resolved
	}
	return fallback
}

// recorderModule is the screen-recorder bell: state and elapsed clock
// from the shared recorder service.
type recorderModule struct {
	ctx   ModuleContext
	src   *recorder.State
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget
}

func newRecorder(ctx ModuleContext) (Module, error) {
	m := &recorderModule{ctx: ctx, src: ctx.Recorder, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	if m.src == nil {
		m.src = recorder.NewState(recorder.WfRecorder{}, 0)
	}
	m.icon = moduleIcon(ctx, ctx.Config.Recorder.Icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	m.refresh()
	// Follow the shared state; headless construction refreshes inline.
	if ctx.App == nil {
		go func() {
			for range m.src.Changes() {
				m.refresh()
			}
		}()
		return m, nil
	}
	go func() {
		for range m.src.Changes() {
			m.ctx.Invoke(m.refresh)
		}
	}()
	return m, nil
}

// refresh applies the state to label and icon.
func (m *recorderModule) refresh() {
	cfg := m.ctx.Config.Recorder
	snap := m.src.Snapshot()
	text := ""
	if cfg.LabelShow {
		text = recorderLabel(cfg.Format, snap.Active, snap.Paused, snap.ElapsedSecs)
	}
	m.label.SetText(text)
	m.label.SetColor(recorderColor(cfg, snap, m.ctx.Style.palette, m.ctx.Style.fg))
	if setter := m.icon; setter != nil {
		setter.SetThemeName(recorderIconName(cfg, snap))
	}
}

func (m *recorderModule) Root() widget.Widget { return m.root }
