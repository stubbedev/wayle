package bar

import (
	"errors"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/recorder"
)

// recorderLabel is helpers.rs's build_label: Idle/Paused/Recording
// for {{ state }}, the H:MM:SS (or M:SS) clock for {{ elapsed }}, a
// dash while idle.
func recorderLabel(format string, active, paused bool, elapsedSecs uint32) string {
	state := i18n.T("bar-recorder-idle")
	switch {
	case active && paused:
		state = i18n.T("bar-recorder-paused")
	case active:
		state = i18n.T("bar-recorder-recording")
	}
	elapsed := "-"
	if active {
		elapsed = recorder.FormatElapsed(elapsedSecs)
	}
	return jinja.RenderOr(format, map[string]any{"state": state, "elapsed": elapsed})
}

// recorderIconName is helpers.rs's select_icon over the state trio.
func recorderIconName(cfg config.RecorderConfig, snap recorder.Change) string {
	switch {
	case snap.Active && snap.Paused:
		return cfg.Icons()[config.RecorderPaused].Name
	case snap.Active, snap.Preparing:
		return cfg.Icons()[config.RecorderRecording].Name
	}
	return cfg.Icons()[config.RecorderIdle].Name
}

// recorderModule is the screen-recorder bell: state and elapsed clock
// from the shared recorder service.
type recorderModule struct {
	ctx   ModuleContext
	src   *recorder.State
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget
	// chrome is the module root the preparing class goes on.
	chrome moduleChrome
}

func newRecorder(ctx ModuleContext) (Module, error) {
	if ctx.Recorder == nil {
		return nil, errors.New("recorder: no recorder service available")
	}
	m := &recorderModule{ctx: ctx, src: ctx.Recorder, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.Recorder.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	m.refresh()
	// Follow the shared state; headless construction refreshes inline.
	changes, stop := m.src.Changes()
	follow(m.ctx, changes, stop, func(recorder.Change) { m.refresh() })
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
	if setter := m.icon; setter != nil {
		setter.SetThemeName(recorderIconName(cfg, snap))
	}
	// The pre-capture delay pulses the icon through the module root's
	// recorder-preparing class.
	setClass(m.chrome, "recorder-preparing", snap.Preparing)
}

func (m *recorderModule) setChrome(c moduleChrome) {
	m.chrome = c
	m.refresh()
}

func (m *recorderModule) Root() widget.Widget { return m.root }
