package bar

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/styling"
)

// wsState is one workspace button's model, derived from the IPC state.
type wsState struct {
	id       int
	name     string
	occupied bool
	active   bool
}

// workspaceModel turns a workspaces+monitors snapshot into the button
// list for one bar's monitor: the monitor's own workspaces sorted by
// id, active flags from the focused monitor's active workspace, and
// the min-workspace padding. Pure so the IPC wiring stays thin and the
// behavior is test covered.
func workspaceModel(all []hyprland.Workspace, monitors []hyprland.Monitor, connector string, cfg config.HyprlandWorkspacesConfig) []wsState {
	byMonitor := make([]hyprland.Workspace, 0, len(all))
	for _, ws := range all {
		if ws.Monitor != connector {
			continue
		}
		if !cfg.ShowSpecial && ws.ID < 0 {
			continue
		}
		byMonitor = append(byMonitor, ws)
	}
	sort.Slice(byMonitor, func(i, j int) bool { return byMonitor[i].ID < byMonitor[j].ID })

	activeIDs := map[int]bool{}
	for _, m := range monitors {
		if m.Focused && m.Name == connector {
			activeIDs[m.ActiveWS.ID] = true
		}
	}

	out := make([]wsState, 0, len(byMonitor))
	for _, ws := range byMonitor {
		out = append(out, wsState{
			id:       ws.ID,
			name:     ws.Name,
			occupied: ws.Windows > 0,
			active:   activeIDs[ws.ID],
		})
	}
	if len(out) < cfg.MinWorkspace {
		// Pad with the lowest ids the monitor does not already show.
		taken := map[int]bool{}
		for _, ws := range out {
			taken[ws.id] = true
		}
		for candidate := 1; len(out) < cfg.MinWorkspace; candidate++ {
			if taken[candidate] {
				continue
			}
			taken[candidate] = true
			out = append(out, wsState{id: candidate, name: strconv.Itoa(candidate)})
		}
		// Padding re-sorts: the pads slot among the real workspaces.
		sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	}
	return out
}

// wsLabel renders one button's text: the number, or the workspace name
// when label-use-name is set. Special workspaces keep their name.
func wsLabel(state wsState, cfg config.HyprlandWorkspacesConfig) string {
	if cfg.LabelUseName || state.id < 0 {
		return state.name
	}
	return strconv.Itoa(state.id)
}

// wsColor resolves a button's color: the workspace map wins, then
// active/occupied/empty by state.
func wsColor(state wsState, cfg config.HyprlandWorkspacesConfig, palette *styling.Palette) render.Color {
	if style, ok := cfg.WorkspaceMap[state.id]; ok && style.ColorSet {
		if color, ok := styling.ResolveColor(style.Color, palette); ok {
			return color
		}
	}
	cv := cfg.EmptyColor
	switch {
	case state.active:
		cv = cfg.ActiveColor
	case state.occupied:
		cv = cfg.OccupiedColor
	}
	color, _ := styling.ResolveColor(cv, palette)
	return color
}

// dispatchWorkspace switches to a workspace by id; special workspaces
// dispatch by name.
func dispatchWorkspace(ctx ModuleContext, id int) {
	if ctx.Hyprland == nil {
		return
	}
	if id < 0 {
		_, _ = ctx.Hyprland.Dispatch("workspace name:special:" + strconv.Itoa(-id))
		return
	}
	_, _ = ctx.Hyprland.Dispatch("workspace " + strconv.Itoa(id))
}

// hyprlandWorkspaces is the module: a row of workspace buttons kept
// current from the event stream. Any workspace-relevant event triggers
// a full re-query — the query is one small unix-socket round trip and
// the buttons are a handful, so the diff lives in gelm's damage
// tracking instead of here.
type hyprlandWorkspaces struct {
	ctx   ModuleContext
	conn  *hyprland.Connection
	row   *widget.Box
	model []wsState
}

func newHyprlandWorkspaces(ctx ModuleContext) (Module, error) {
	if ctx.Hyprland == nil {
		return nil, errors.New("hyprland-workspaces: the compositor is not Hyprland")
	}
	m := &hyprlandWorkspaces{ctx: ctx, conn: ctx.Hyprland}
	m.row = widget.NewBox(widget.Row, 0, 0)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if ctx.App == nil {
		return m, nil
	}
	events, err := ctx.Hyprland.Events(context.Background())
	if err != nil {
		return nil, fmt.Errorf("hyprland-workspaces: events: %w", err)
	}
	go m.pumpEvents(events)
	return m, nil
}

// pumpEvents forwards stream events onto the loop goroutine; the
// model refresh itself runs through Invoke so the widget tree is only
// ever touched from the loop.
func (m *hyprlandWorkspaces) pumpEvents(events <-chan hyprland.Event) {
	for range events {
		m.ctx.Invoke(func() {
			if err := m.refresh(); err != nil {
				return
			}
		})
	}
}

// refresh re-queries the state and swaps in a freshly built row: the
// container keeps its identity in the tree, the row rebuilds.
func (m *hyprlandWorkspaces) refresh() error {
	workspaces, err := m.conn.Workspaces()
	if err != nil {
		return err
	}
	monitors, err := m.conn.Monitors()
	if err != nil {
		return err
	}
	m.model = workspaceModel(workspaces, monitors, m.ctx.Connector, m.ctx.Config.HyprlandWorkspaces)
	m.row.Clear()
	m.row.Append(m.buildRow(), false)
	m.row.InvalidateLayout()
	return nil
}

// buildRow lays one model out as a row of labeled buttons.
func (m *hyprlandWorkspaces) buildRow() widget.Widget {
	cfg := m.ctx.Config.HyprlandWorkspaces
	gap := int(math.Round(cfg.WorkspacePad.ResolvePx(styling.RemBase, m.ctx.Config.Bar.Scale)))
	labelPx := m.labelPx()
	row := widget.NewBox(widget.Row, gap, 0)
	for _, state := range m.model {
		color := wsColor(state, cfg, m.ctx.Style.palette)
		label := widget.NewLabel(m.ctx.Font, labelPx, wsLabel(state, cfg), color)
		btn := widget.NewButton(label, 4, styling.RoundingRadiusPx(config.RoundingSm, m.ctx.Config.Bar.Scale))
		if state.active && cfg.ActiveIndicator == config.ActiveBackground {
			btn.Bg = color
		}
		id := state.id
		btn.OnClick = func() { dispatchWorkspace(m.ctx, id) }
		row.Append(btn, false)
	}
	return row
}

// labelPx resolves the label size against the bar scale.
func (m *hyprlandWorkspaces) labelPx() float64 {
	cfg := m.ctx.Config.HyprlandWorkspaces
	if cfg.LabelSize.Unit == config.SizePixels {
		return cfg.LabelSize.Value
	}
	return cfg.LabelSize.Value * styling.RemBase * m.ctx.Config.Bar.Scale
}

func (m *hyprlandWorkspaces) Root() widget.Widget { return m.row }
