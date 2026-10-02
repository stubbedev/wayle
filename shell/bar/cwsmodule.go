package bar

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/spawn"
	"github.com/stubbedev/wayle/styling"
)

// cwsBlinkInterval is BLINK_INTERVAL: the urgent pulse half-period.
const cwsBlinkInterval = 500 * time.Millisecond

const (
	// cwsSpaceLgRem is tokens.scss's $base-space-lg, the .workspace
	// min-width/min-height (--bar-space-lg).
	cwsSpaceLgRem = 1.5
)

// cwsBackend is one compositor's side of the module: the snapshot,
// the focus actions the click bindings map to, and the event stream.
type cwsBackend interface {
	snapshot() ([]cwsWorkspace, []cwsWindow, error)
	focus(ctx context.Context, ws cwsWorkspace) error
	focusNext(ctx context.Context) error
	focusPrevious(ctx context.Context) error
	focusLast(ctx context.Context) error
	subscribe() (<-chan struct{}, func(), error)
	close()
}

// cwsActions are the compositor requests the bindings map to.
type cwsActions struct {
	focus    func(ctx context.Context, ws cwsWorkspace) error
	next     func(ctx context.Context) error
	previous func(ctx context.Context) error
	last     func(ctx context.Context) error
}

// cwsView is what the container and button widgets read: the module
// context and shared config, the resolved face, and the actions. The
// sway/niri/mango module and the hyprland module each carry one.
type cwsView struct {
	ctx      ModuleContext
	cfg      config.CompositorWorkspacesConfig
	kind     string
	flavor   cwsFlavor
	boldFace render.Font
	root     *cwsContainer
	actions  cwsActions
	// otherMonitor is hyprland's active-on-other-monitor-color.
	otherMonitor config.ColorValue
}

// cwsModule is the sway/niri/mango workspaces component: the container,
// its buttons rebuilt from the latest snapshot, and the urgent blink.
type cwsModule struct {
	stopOnce sync.Once
	cwsView
	backend cwsBackend

	workspaces []cwsWorkspace
	windows    []cwsWindow
	models     []cwsButtonModel

	blinkOn    bool
	blinkStop  func()
	urgentSeen bool
	stopEvents func()
}

// newCwsModule builds the module over a connected backend and, with a
// live loop, subscribes to its events.
func newCwsModule(ctx ModuleContext, kind string, flavor cwsFlavor, cfg config.CompositorWorkspacesConfig, backend cwsBackend) (*cwsModule, error) {
	m := &cwsModule{ctx: ctx, cfg: cfg, kind: kind, flavor: flavor, backend: backend}
	m.actions = cwsActions{focus: backend.focus, next: backend.focusNext, previous: backend.focusPrevious, last: backend.focusLast}
	m.boldFace = cwsBoldFace(ctx, m.labelPx())
	m.root = newCwsContainer(&m.cwsView)
	if err := m.refresh(); err != nil {
		backend.close()
		return nil, err
	}
	if ctx.App == nil {
		return m, nil
	}
	ticks, stop, err := backend.subscribe()
	if err != nil {
		backend.close()
		return nil, err
	}
	m.stopEvents = stop
	follow(m.ctx, ticks, func() { m.Stop() }, func(struct{}) {
		if err := m.refresh(); err != nil {
			log.Printf("%s-workspaces: %v", m.kind, err)
		}
	})
	return m, nil
}

// cwsBoldFace resolves the .workspace-label face: font-sans at
// --weight-bold. Headless contexts (tests) keep the context's face.
func cwsBoldFace(ctx ModuleContext, px float64) render.Font {
	if ctx.App == nil || ctx.Config == nil || px <= 0 {
		return ctx.Font
	}
	face, err := app.FontWeighted(ctx.Config.General.FontSans, px, 700, false)
	if err != nil {
		return ctx.Font
	}
	return app.FontFallback(face)
}

// refresh re-queries the compositor and rebuilds (the WorkspacesChanged
// arm of update_cmd).
func (m *cwsModule) refresh() error {
	workspaces, windows, err := m.backend.snapshot()
	if err != nil {
		return err
	}
	m.workspaces, m.windows = workspaces, windows
	m.rebuild()
	m.syncBlink()
	return nil
}

// rebuild recomputes the button models from the cached snapshot and
// swaps the buttons in.
func (m *cwsModule) rebuild() {
	m.models = cwsBuildModels(m.flavor, m.workspaces, m.windows, m.cfg, m.ctx.Connector, m.vertical(), m.blinkOn)
	displayed := make([]cwsWorkspace, len(m.models))
	for i, model := range m.models {
		displayed[i] = model.ws
	}
	m.urgentSeen = m.cfg.UrgentShow && cwsAnyUrgent(displayed, m.windows)
	m.root.setButtons(m.models)
}

// syncBlink is sync_blink: the pulse runs exactly while something
// displayed is urgent.
func (m *cwsModule) syncBlink() {
	switch {
	case m.urgentSeen && m.blinkStop == nil:
		m.startBlink()
	case !m.urgentSeen && m.blinkStop != nil:
		m.stopBlink()
	}
}

// startBlink is start_blink_timer: on immediately, then toggling.
func (m *cwsModule) startBlink() {
	m.stopBlink()
	m.blinkOn = true
	m.rebuild()
	if m.ctx.App == nil {
		m.blinkStop = func() {}
		return
	}
	m.blinkStop = m.ctx.App.Every(cwsBlinkInterval, m.blinkTick)
}

// blinkTick is the BlinkTick arm: flip and rebuild from the cache.
func (m *cwsModule) blinkTick() {
	m.blinkOn = !m.blinkOn
	m.rebuild()
}

func (m *cwsModule) stopBlink() {
	if m.blinkStop != nil {
		m.blinkStop()
		m.blinkStop = nil
	}
	if m.blinkOn {
		m.blinkOn = false
		m.rebuild()
	}
}

// vertical reports a side-docked bar.
func (m *cwsView) vertical() bool {
	if m.ctx.Config == nil {
		return false
	}
	loc := m.ctx.Config.Bar.Location
	return loc == config.LocationLeft || loc == config.LocationRight
}

func (m *cwsView) scale() float64 {
	if m.ctx.Config == nil || m.ctx.Config.Bar.Scale <= 0 {
		return 1
	}
	return float64(m.ctx.Config.Bar.Scale)
}

// labelPx is label-size resolved against LABEL_BASE_REM.
func (m *cwsView) labelPx() float64 {
	return math.Round(m.cfg.LabelSize.ResolvePx(config.WorkspaceLabelBaseRem*styling.RemBase, m.scale()))
}

// iconPx is icon-size resolved against ICON_BASE_REM.
func (m *cwsView) iconPx() int {
	return int(math.Round(m.cfg.IconSize.ResolvePx(config.WorkspaceIconBaseRem*styling.RemBase, m.scale())))
}

// iconGapPx is methods.rs's icon_gap_px: a scale is rem without the
// bar scale, pixels are literal.
func (m *cwsView) iconGapPx() int {
	if m.cfg.IconGap.Unit == config.SizePixels {
		return int(math.Round(float64(m.cfg.IconGap.Value)))
	}
	return int(math.Round(float64(m.cfg.IconGap.Value) * styling.RemBase))
}

// paddingPx is workspace-padding at a 1 rem base.
func (m *cwsView) paddingPx() int {
	return int(math.Round(m.cfg.WorkspacePad.ResolvePx(styling.RemBase, m.scale())))
}

// dispatchClick is dispatch_click_action for a clicked workspace.
func (m *cwsView) dispatchClick(action config.WorkspaceClickAction, ws cwsWorkspace) {
	if action.Kind == config.WorkspaceClickFocusThis {
		m.run(func(ctx context.Context) error { return m.actions.focus(ctx, ws) })
		return
	}
	m.dispatchScroll(action)
}

// dispatchScroll is dispatch_scroll_action: focus:this needs a clicked
// workspace, so scrolling ignores it.
func (m *cwsView) dispatchScroll(action config.WorkspaceClickAction) {
	switch action.Kind {
	case config.WorkspaceClickNone:
	case config.WorkspaceClickFocusThis:
		log.Printf("%s-workspaces: focus:this requires a clicked workspace; scroll ignored", m.kind)
	case config.WorkspaceClickFocusNext:
		m.run(m.actions.next)
	case config.WorkspaceClickFocusPrevious:
		m.run(m.actions.previous)
	case config.WorkspaceClickFocusLast:
		m.run(m.actions.last)
	case config.WorkspaceClickDropdown:
		if m.ctx.Dropdowns != nil {
			if err := m.ctx.Dropdowns.open(m.ctx.Connector, action.Arg, m.root); err != nil {
				log.Printf("%s-workspaces: dropdown %q: %v", m.kind, action.Arg, err)
			}
		}
	case config.WorkspaceClickShell:
		_ = spawn.Quiet(action.Arg) // process::run_if_set
	}
}

// run fires one compositor request off the loop goroutine, like the
// Rust tokio::spawn, logging a failure.
func (m *cwsView) run(fn func(context.Context) error) {
	go func() {
		if err := fn(context.Background()); err != nil {
			log.Printf("%s-workspaces: %v", m.kind, err)
		}
	}()
}

func (m *cwsModule) Root() widget.Widget { return m.root }

// ownsChrome opts out of the module-button wrapper: the workspaces
// component is not a BarButton in the Rust shell.
func (m *cwsModule) ownsChrome() {}

// Stop releases the timer and the IPC.
func (m *cwsModule) Stop() {
	m.stopOnce.Do(func() {
		if m.blinkStop != nil {
			m.blinkStop()
			m.blinkStop = nil
		}
		if m.stopEvents != nil {
			m.stopEvents()
		}
		m.backend.close()
	})
}
