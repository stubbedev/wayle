package bar

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sway"
	"github.com/stubbedev/wayle/styling"
)

// swayWorkspaces is the sway twin of the hyprland module: a row of
// workspace buttons driven by sway's i3 IPC event stream. The shared
// HyprlandWorkspacesConfig carries the styling (the schema's
// SwayWorkspacesConfig has the same shape).
type swayWorkspaces struct {
	ctx    ModuleContext
	conn   *sway.Conn
	row    *widget.Box
	cancel func()
}

func newSwayWorkspaces(ctx ModuleContext) (Module, error) {
	if !sway.IsRunning() {
		return nil, errors.New("sway-workspaces: sway is not running")
	}
	conn, err := sway.Connect()
	if err != nil {
		return nil, err
	}
	m := &swayWorkspaces{ctx: ctx, conn: conn}
	m.row = widget.NewBox(widget.Row, 0, 0)
	if err := m.refresh(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ctx.App == nil {
		return m, nil
	}
	ticks, stop, err := sway.Subscribe(context.Background())
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	m.cancel = stop
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
	}()
	return m, nil
}

// refresh re-queries and swaps the row.
func (m *swayWorkspaces) refresh() error {
	workspaces, err := m.conn.Workspaces()
	if err != nil {
		return err
	}
	m.row.Clear()
	m.row.Append(m.buildRow(workspaces), false)
	m.row.InvalidateLayout()
	return nil
}

// buildRow lays the model out; the focused workspace is the active
// one, occupied means any window (sway's representation flag).
func (m *swayWorkspaces) buildRow(workspaces []sway.Workspace) widget.Widget {
	cfg := m.ctx.Config.HyprlandWorkspaces
	gap := int(m.ctx.Config.HyprlandWorkspaces.WorkspacePad.ResolvePx(styling.RemBase, m.ctx.Config.Bar.Scale))
	labelPx := m.labelPx()
	// Monitor-specific keeps this bar's own output's workspaces.
	visible := make([]sway.Workspace, 0, len(workspaces))
	for _, ws := range workspaces {
		if cfg.MonitorSpecific && ws.Output != m.ctx.Connector && ws.Output != "" {
			continue
		}
		visible = append(visible, ws)
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].Num < visible[j].Num })
	row := widget.NewBox(widget.Row, gap, 0)
	for _, ws := range visible {
		state := wsState{
			id:       ws.Num,
			name:     ws.Name,
			occupied: ws.Visible,
			active:   ws.Focused,
		}
		color := wsColor(state, cfg, m.ctx.Style.palette)
		label := widget.NewLabel(m.ctx.Font, labelPx, wsLabel(state, cfg), color)
		btn := widget.NewButton(label, 4, styling.RoundingRadiusPx(config.RoundingSm, m.ctx.Config.Bar.Scale))
		if state.active && cfg.ActiveIndicator == config.ActiveBackground {
			btn.Bg = color
		}
		name := ws.Name
		btn.OnClick = func() { _ = m.conn.RunCommand(context.Background(), "workspace "+strconv.Quote(name)) }
		row.Append(btn, false)
	}
	return row
}

// labelPx resolves the label size against the bar scale.
func (m *swayWorkspaces) labelPx() float64 {
	cfg := m.ctx.Config.HyprlandWorkspaces
	if cfg.LabelSize.Unit == config.SizePixels {
		return cfg.LabelSize.Value
	}
	return cfg.LabelSize.Value * styling.RemBase * m.ctx.Config.Bar.Scale
}

func (m *swayWorkspaces) Root() widget.Widget { return m.row }

// Stop releases the IPC connections.
func (m *swayWorkspaces) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.conn != nil {
		_ = m.conn.Close()
	}
}
