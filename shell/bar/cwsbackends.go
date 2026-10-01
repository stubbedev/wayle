package bar

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/service/mango"
	"github.com/stubbedev/wayle/service/niri"
	"github.com/stubbedev/wayle/service/sway"
)

// newSwayWorkspaces is the sway-workspaces factory.
func newSwayWorkspaces(ctx ModuleContext) (Module, error) {
	if !sway.IsRunning() {
		return nil, errors.New("sway-workspaces: sway is not running")
	}
	conn, err := sway.Connect()
	if err != nil {
		return nil, err
	}
	return newCwsModule(ctx, "sway", cwsWorkspaces, ctx.Config.SwayWorkspaces.View(), &swayBackend{conn: conn})
}

// newNiriWorkspaces is the niri-workspaces factory.
func newNiriWorkspaces(ctx ModuleContext) (Module, error) {
	if !niri.IsRunning() {
		return nil, errors.New("niri-workspaces: niri is not running")
	}
	conn, err := niri.Connect()
	if err != nil {
		return nil, err
	}
	return newCwsModule(ctx, "niri", cwsWorkspaces, ctx.Config.NiriWorkspaces.View(), &niriBackend{conn: conn})
}

// swayBackend maps sway's GET_WORKSPACES/GET_TREE onto the shared
// model: occupied means any window's enclosing workspace, active is
// sway's visible flag, and every workspace carries its name and
// output.
type swayBackend struct {
	conn *sway.Conn
	mu   sync.Mutex
	byID map[uint64]sway.Workspace
}

func (b *swayBackend) snapshot() ([]cwsWorkspace, []cwsWindow, error) {
	replies, err := b.conn.Workspaces()
	if err != nil {
		return nil, nil, err
	}
	tree, err := b.conn.Windows()
	if err != nil {
		return nil, nil, err
	}
	windows := make([]cwsWindow, 0, len(tree))
	occupied := map[uint64]bool{}
	for _, w := range tree {
		cw := cwsWindow{
			id:           uint64(w.ID),
			workspace:    uint64(w.WorkspaceID),
			hasWorkspace: w.HasWorkspace,
			urgent:       w.Urgent,
			app:          appicons.Window{AppID: w.AppID, HasAppID: w.HasAppID, Title: w.Title, HasTitle: w.HasTitle},
		}
		if w.HasWorkspace {
			occupied[cw.workspace] = true
		}
		windows = append(windows, cw)
	}
	byID := make(map[uint64]sway.Workspace, len(replies))
	workspaces := make([]cwsWorkspace, 0, len(replies))
	for _, r := range replies {
		id := uint64(r.ID)
		byID[id] = r
		workspaces = append(workspaces, cwsWorkspace{
			id: id, num: r.Num, name: r.Name, hasName: true,
			output: r.Output, hasOutput: true,
			urgent: r.Urgent, active: r.Visible, focused: r.Focused,
			hasWindows: occupied[id],
		})
	}
	b.mu.Lock()
	b.byID = byID
	b.mu.Unlock()
	return workspaces, windows, nil
}

// focus is focus_workspace(WorkspaceRef::Id): resolved against the
// snapshot, or a con_id focus for an id the snapshot lost.
func (b *swayBackend) focus(ctx context.Context, ws cwsWorkspace) error {
	b.mu.Lock()
	reply, ok := b.byID[ws.id]
	b.mu.Unlock()
	if !ok {
		return b.conn.RunCommand(ctx, "[con_id="+strconv.FormatUint(ws.id, 10)+"] focus")
	}
	return b.conn.FocusWorkspace(ctx, reply)
}

func (b *swayBackend) focusNext(ctx context.Context) error { return b.conn.FocusNextOnOutput(ctx) }

func (b *swayBackend) focusPrevious(ctx context.Context) error { return b.conn.FocusPrevOnOutput(ctx) }

func (b *swayBackend) focusLast(ctx context.Context) error { return b.conn.FocusBackAndForth(ctx) }

func (b *swayBackend) subscribe() (<-chan struct{}, func(), error) {
	return sway.Subscribe()
}
func (b *swayBackend) close() { _ = b.conn.Close() }

// niriBackend maps niri's Workspaces/Windows: occupied is an active
// window id, the idx is the number, and windows order by their
// scrolling-layout position.
type niriBackend struct {
	conn *niri.Conn
}

func (b *niriBackend) snapshot() ([]cwsWorkspace, []cwsWindow, error) {
	replies, err := b.conn.Workspaces()
	if err != nil {
		return nil, nil, err
	}
	all, err := b.conn.Windows()
	if err != nil {
		return nil, nil, err
	}
	workspaces := make([]cwsWorkspace, 0, len(replies))
	for _, r := range replies {
		ws := cwsWorkspace{
			id: r.ID, num: int(r.Idx),
			urgent: r.IsUrgent, active: r.IsActive, focused: r.IsFocused,
			hasWindows: r.ActiveWindowID != nil,
		}
		if r.Name != nil {
			ws.name, ws.hasName = *r.Name, true
		}
		if r.Output != nil {
			ws.output, ws.hasOutput = *r.Output, true
		}
		workspaces = append(workspaces, ws)
	}
	windows := make([]cwsWindow, 0, len(all))
	for _, w := range all {
		cw := cwsWindow{id: w.ID, urgent: w.IsUrgent}
		if w.WorkspaceID != nil {
			cw.workspace, cw.hasWorkspace = *w.WorkspaceID, true
		}
		if w.AppID != nil {
			cw.app.AppID, cw.app.HasAppID = *w.AppID, true
		}
		if w.Title != nil {
			cw.app.Title, cw.app.HasTitle = *w.Title, true
		}
		// (column, tile), floating windows (no position) last.
		cw.order = [2]int{int(^uint(0) >> 1), 0}
		if pos := w.Layout.PosInScrollingLayout; pos != nil {
			cw.order = *pos
		}
		windows = append(windows, cw)
	}
	return workspaces, windows, nil
}

func (b *niriBackend) focus(ctx context.Context, ws cwsWorkspace) error {
	return b.conn.FocusWorkspaceID(ctx, ws.id)
}

func (b *niriBackend) focusNext(ctx context.Context) error { return b.conn.FocusWorkspaceDown(ctx) }

func (b *niriBackend) focusPrevious(ctx context.Context) error { return b.conn.FocusWorkspaceUp(ctx) }

func (b *niriBackend) focusLast(ctx context.Context) error {
	return b.conn.FocusWorkspacePrevious(ctx)
}
func (b *niriBackend) subscribe() (<-chan struct{}, func(), error) { return niri.Subscribe() }
func (b *niriBackend) close()                                      { _ = b.conn.Close() }

// newMangoWorkspaces is the mango-workspaces factory: the watcher runs
// from construction, like MangoService::new's start_monitoring.
func newMangoWorkspaces(ctx ModuleContext) (Module, error) {
	if !mango.IsRunning() {
		return nil, errors.New("mango-workspaces: mango is not running")
	}
	w, err := mango.Watch()
	if err != nil {
		return nil, err
	}
	cfg := ctx.Config.MangoWorkspaces
	backend := &mangoBackend{watch: w, connector: ctx.Connector, hideEmpty: cfg.HideEmpty, minTagCount: int(cfg.MinTagCount)}
	return newCwsModule(ctx, "mango", cwsTags, cfg.View(), backend)
}

// mangoBackend selects one monitor's tags (rebuild_tags): the bar's
// own monitor, else the active one, else the first; hide-empty keeps
// occupied, active, and the first min-tag-count tags. Clients map to
// one window per tag they sit on, in mango's list order.
type mangoBackend struct {
	watch       *mango.Watcher
	connector   string
	hideEmpty   bool
	minTagCount int
}

// chooseMonitor is chosen_monitor.
func chooseMonitor(monitors []mango.Monitor, connector string) (mango.Monitor, bool) {
	if connector != "" {
		for _, m := range monitors {
			if m.Name == connector {
				return m, true
			}
		}
	}
	for _, m := range monitors {
		if m.IsActive {
			return m, true
		}
	}
	if len(monitors) > 0 {
		return monitors[0], true
	}
	return mango.Monitor{}, false
}

func (b *mangoBackend) snapshot() ([]cwsWorkspace, []cwsWindow, error) {
	monitors, clients := b.watch.State()
	monitor, ok := chooseMonitor(monitors, b.connector)
	if !ok {
		return nil, nil, nil
	}
	tags := make([]cwsWorkspace, 0, len(monitor.Tags))
	for _, tag := range monitor.Tags {
		// displayed_tags
		if b.hideEmpty && tag.ClientCount == 0 && !tag.IsActive && int(tag.Index) > b.minTagCount {
			continue
		}
		tags = append(tags, cwsWorkspace{
			id: uint64(tag.Index), num: int(tag.Index),
			output: monitor.Name, hasOutput: true,
			urgent: tag.IsUrgent, active: tag.IsActive,
			hasWindows: tag.ClientCount > 0,
		})
	}
	windows := make([]cwsWindow, 0, len(clients))
	for pos, c := range clients {
		if c.Monitor != monitor.Name {
			continue
		}
		for _, tag := range c.Tags {
			windows = append(windows, cwsWindow{
				id: uint64(c.ID), workspace: uint64(tag), hasWorkspace: true,
				urgent: c.IsUrgent, order: [2]int{pos, 0},
				app: appicons.Window{AppID: c.AppID, HasAppID: c.HasAppID, Title: c.Title, HasTitle: c.HasTitle},
			})
		}
	}
	return tags, windows, nil
}

func (b *mangoBackend) focus(_ context.Context, ws cwsWorkspace) error {
	return mango.ViewTag(uint32(ws.id))
}
func (b *mangoBackend) focusNext(context.Context) error     { return mango.ViewRight() }
func (b *mangoBackend) focusPrevious(context.Context) error { return mango.ViewLeft() }

// focusLast is a no-op: mango has no last-tag action.
func (b *mangoBackend) focusLast(context.Context) error { return nil }

func (b *mangoBackend) subscribe() (<-chan struct{}, func(), error) {
	return b.watch.Ticks(), b.watch.Close, nil
}
func (b *mangoBackend) close() { b.watch.Close() }
