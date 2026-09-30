package modes

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/launcher"
	"github.com/stubbedev/wayle/service/niri"
	"github.com/stubbedev/wayle/service/sway"
)

// WindowField is a window field fed to the matcher
// (-window-match-fields).
type WindowField uint8

// Window match fields.
const (
	WindowFieldTitle WindowField = iota
	WindowFieldClass
	WindowFieldName
	WindowFieldRole
	WindowFieldDesktop
)

// WindowConfig is the window mode's knobs (rofi's -window-* family).
type WindowConfig struct {
	// Format is the row template: {w} workspace, {c} class, {t} title,
	// {n} name, {r} role.
	Format      string
	MatchFields []WindowField
	// HideActive leaves the focused window out.
	HideActive bool
	// CloseOnDelete makes shift-delete close the window.
	CloseOnDelete bool
	// WindowCommand is the alt accept command, {window} substituted.
	WindowCommand string
	// CurrentDesktopOnly restricts the list to the current workspace
	// (windowcd).
	CurrentDesktopOnly bool
}

// DefaultWindowConfig is WindowConfig::default.
func DefaultWindowConfig() WindowConfig {
	return WindowConfig{
		Format:        "{w}   {c}   {t}",
		MatchFields:   []WindowField{WindowFieldTitle, WindowFieldClass},
		CloseOnDelete: true,
	}
}

// windowInfo is one open window, compositor-agnostic.
type windowInfo struct {
	// id is the compositor's id: hyprland address, niri id, sway con_id.
	id        string
	title     string
	class     string
	workspace string
	focused   bool
	// focusOrder is lower for more recently focused windows.
	focusOrder         int64
	onCurrentWorkspace bool
}

// windowBackend talks to one compositor. The Rust mode shelled out to
// hyprctl, niri msg, and swaymsg; the Go shell already has native IPC
// clients for all three, so the same JSON arrives over the sockets.
type windowBackend interface {
	list(ctx context.Context) ([]windowInfo, error)
	focus(ctx context.Context, id string) error
	close(ctx context.Context, id string) error
}

// detectBackend picks the compositor from the environment, in the Rust
// order: hyprland, niri, sway.
func detectBackend() windowBackend {
	switch {
	case os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "":
		return hyprlandBackend{}
	case os.Getenv("NIRI_SOCKET") != "":
		return niriBackend{}
	case os.Getenv("SWAYSOCK") != "":
		return swayBackend{}
	}
	return nil
}

// Window switches between open windows (window.rs).
type Window struct {
	cfg     WindowConfig
	backend windowBackend
	windows []windowInfo
}

// NewWindow builds the mode; the compositor is detected from the
// environment.
func NewWindow(cfg WindowConfig) *Window { return &Window{cfg: cfg, backend: detectBackend()} }

// Name is window or windowcd.
func (w *Window) Name() string {
	if w.cfg.CurrentDesktopOnly {
		return "windowcd"
	}
	return "window"
}

// Load lists the windows, most recently focused first; the focused one
// sorts last so plain Enter switches to the previous window, as rofi.
func (w *Window) Load(ctx context.Context) launcher.ModeState {
	var windows []windowInfo
	if w.backend == nil {
		log.Printf("launcher: window mode: no supported compositor detected")
	} else {
		var err error
		if windows, err = w.backend.list(ctx); err != nil {
			log.Printf("launcher: window mode: %v", err)
		}
	}
	windows = slices.DeleteFunc(windows, func(win windowInfo) bool {
		return (w.cfg.HideActive && win.focused) || (w.cfg.CurrentDesktopOnly && !win.onCurrentWorkspace)
	})
	slices.SortStableFunc(windows, func(a, b windowInfo) int {
		if a.focused != b.focused {
			if a.focused {
				return 1
			}
			return -1
		}
		switch {
		case a.focusOrder < b.focusOrder:
			return -1
		case a.focusOrder > b.focusOrder:
			return 1
		}
		return 0
	})
	items := make([]launcher.Item, len(windows))
	for i, win := range windows {
		items[i] = windowItem(win, w.cfg)
	}
	w.windows = windows
	return launcher.ModeState{Items: items, Prompt: w.Name(), NoCustom: true}
}

// Activate focuses the window, or runs -window-command on Alt.
func (w *Window) Activate(ctx context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	i, ok := target.Row()
	if w.backend == nil || !ok || int(i) >= len(w.windows) {
		return launcher.ActionNothing{}
	}
	win := w.windows[i]
	if _, alt := kind.(launcher.ActivateAlt); alt && w.cfg.WindowCommand != "" {
		launcher.RunShell(launcher.Render(w.cfg.WindowCommand, launcher.Values(map[string]string{"window": win.id})))
		return launcher.ActionClose{}
	}
	if err := w.backend.focus(ctx, win.id); err != nil {
		log.Printf("launcher: focus window: %v", err)
	}
	return launcher.ActionClose{}
}

// Delete closes the window (when close-on-delete) and reloads.
func (w *Window) Delete(ctx context.Context, index uint32) launcher.Action {
	if !w.cfg.CloseOnDelete || w.backend == nil || int(index) >= len(w.windows) {
		return launcher.ActionNothing{}
	}
	if err := w.backend.close(ctx, w.windows[index].id); err != nil {
		log.Printf("launcher: close window: %v", err)
	}
	// Give the compositor a beat before the reload re-queries.
	time.Sleep(50 * time.Millisecond)
	return launcher.ActionReload{State: w.Load(ctx)}
}

// AllowsCustom is false.
func (*Window) AllowsCustom() bool { return false }

func windowItem(win windowInfo, cfg WindowConfig) launcher.Item {
	display := launcher.Render(cfg.Format, launcher.Values(map[string]string{
		"w": win.workspace, "c": win.class, "t": win.title, "n": win.class, "r": "",
	}))
	parts := make([]string, 0, len(cfg.MatchFields))
	for _, f := range cfg.MatchFields {
		switch f {
		case WindowFieldTitle:
			parts = append(parts, win.title)
		case WindowFieldDesktop:
			parts = append(parts, win.workspace)
		default:
			parts = append(parts, win.class)
		}
	}
	id := win.id
	return launcher.Item{
		Display:   display,
		MatchText: strings.Join(parts, " "),
		Icon:      launcher.IconName(strings.ToLower(win.class)),
		Info:      &id,
	}
}

type hyprlandBackend struct{}

// hyprClient is the part of j/clients the mode reads.
type hyprClient struct {
	Address   string `json:"address"`
	Title     string `json:"title"`
	Class     string `json:"class"`
	Mapped    *bool  `json:"mapped"`
	Workspace struct {
		ID   *int64 `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	FocusHistoryID *int64 `json:"focusHistoryID"`
}

func (hyprlandBackend) conn() (*hyprland.Connection, error) { return hyprland.Connect() }

func (b hyprlandBackend) list(context.Context) ([]windowInfo, error) {
	c, err := b.conn()
	if err != nil {
		return nil, err
	}
	raw, err := c.Command("j/clients")
	if err != nil {
		return nil, err
	}
	var active *int64
	if ws, err := c.Command("j/activeworkspace"); err == nil {
		var w struct {
			ID *int64 `json:"id"`
		}
		if json.Unmarshal([]byte(ws), &w) == nil {
			active = w.ID
		}
	}
	return parseHyprlandClients([]byte(raw), active)
}

func parseHyprlandClients(raw []byte, active *int64) ([]windowInfo, error) {
	var clients []hyprClient
	if err := json.Unmarshal(raw, &clients); err != nil {
		return nil, err
	}
	var out []windowInfo
	for _, c := range clients {
		if c.Mapped != nil && !*c.Mapped {
			continue
		}
		order := int64(1<<63 - 1)
		if c.FocusHistoryID != nil {
			order = *c.FocusHistoryID
		}
		out = append(out, windowInfo{
			id: c.Address, title: c.Title, class: c.Class, workspace: c.Workspace.Name,
			focused:            c.FocusHistoryID != nil && *c.FocusHistoryID == 0,
			focusOrder:         order,
			onCurrentWorkspace: c.Workspace.ID != nil && active != nil && *c.Workspace.ID == *active,
		})
	}
	return out, nil
}

func (b hyprlandBackend) focus(_ context.Context, id string) error {
	c, err := b.conn()
	if err != nil {
		return err
	}
	_, err = c.Dispatch("focuswindow address:" + id)
	return err
}

func (b hyprlandBackend) close(_ context.Context, id string) error {
	c, err := b.conn()
	if err != nil {
		return err
	}
	_, err = c.Dispatch("closewindow address:" + id)
	return err
}

type niriBackend struct{}

func (niriBackend) list(context.Context) ([]windowInfo, error) {
	c, err := niri.Connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	windows, err := c.Windows()
	if err != nil {
		return nil, err
	}
	return niriWindows(windows), nil
}

// niriWindows maps niri's list; niri reports no MRU, so list order
// stands in for focus order.
func niriWindows(windows []niri.Window) []windowInfo {
	var focusedWS *uint64
	for _, w := range windows {
		if w.IsFocused {
			focusedWS = w.WorkspaceID
			break
		}
	}
	out := make([]windowInfo, len(windows))
	for i, w := range windows {
		ws := ""
		if w.WorkspaceID != nil {
			ws = strconv.FormatUint(*w.WorkspaceID, 10)
		}
		sameWS := (w.WorkspaceID == nil && focusedWS == nil) ||
			(w.WorkspaceID != nil && focusedWS != nil && *w.WorkspaceID == *focusedWS)
		out[i] = windowInfo{
			id: strconv.FormatUint(w.ID, 10), title: deref(w.Title), class: deref(w.AppID), workspace: ws,
			focused: w.IsFocused, focusOrder: int64(i), onCurrentWorkspace: sameWS,
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (niriBackend) action(ctx context.Context, name, id string) error {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return err
	}
	c, err := niri.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.Action(ctx, name, map[string]any{"id": n})
}

func (b niriBackend) focus(ctx context.Context, id string) error {
	return b.action(ctx, "FocusWindow", id)
}

func (b niriBackend) close(ctx context.Context, id string) error {
	return b.action(ctx, "CloseWindow", id)
}

type swayBackend struct{}

func (swayBackend) list(context.Context) ([]windowInfo, error) {
	c, err := sway.Connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	raw, err := c.Tree()
	if err != nil {
		return nil, err
	}
	return parseSwayTree(raw)
}

// swayNode is the part of a GET_TREE node the walk reads; raw fields
// keep "present and a string" distinct from absent or null, as the
// Rust walk tests them.
type swayNode struct {
	Type             string            `json:"type"`
	ID               int64             `json:"id"`
	Name             json.RawMessage   `json:"name"`
	AppID            json.RawMessage   `json:"app_id"`
	WindowProperties json.RawMessage   `json:"window_properties"`
	Focused          bool              `json:"focused"`
	Nodes            []json.RawMessage `json:"nodes"`
	FloatingNodes    []json.RawMessage `json:"floating_nodes"`
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func parseSwayTree(raw []byte) ([]windowInfo, error) {
	var out []windowInfo
	if err := walkSway(raw, "", false, &out); err != nil {
		return nil, err
	}
	focusedWS, found := "", false
	for _, w := range out {
		if w.focused {
			focusedWS, found = w.workspace, true
			break
		}
	}
	for i := range out {
		out[i].onCurrentWorkspace = found && out[i].workspace == focusedWS
	}
	return out, nil
}

// walkSway collects view nodes, carrying the nearest workspace name. A
// view is a con or floating_con leaf with a string name and either an
// app_id string or window_properties object (window.rs walk_sway).
func walkSway(raw json.RawMessage, workspace string, hasWS bool, out *[]windowInfo) error {
	var n swayNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	if n.Type == "workspace" {
		if name, ok := jsonString(n.Name); ok {
			workspace, hasWS = name, true
		} else {
			workspace, hasWS = "", false
		}
	}
	appID, hasAppID := jsonString(n.AppID)
	props := len(n.WindowProperties) > 0 && n.WindowProperties[0] == '{'
	name, hasName := jsonString(n.Name)
	if (n.Type == "con" || n.Type == "floating_con") && (hasAppID || props) && hasName && len(n.Nodes) == 0 {
		class := appID
		if !hasAppID && props {
			var wp struct {
				Class *string `json:"class"`
			}
			_ = json.Unmarshal(n.WindowProperties, &wp)
			class = deref(wp.Class)
		}
		ws := ""
		if hasWS {
			ws = workspace
		}
		*out = append(*out, windowInfo{
			id: strconv.FormatInt(n.ID, 10), title: name, class: class, workspace: ws,
			focused: n.Focused, focusOrder: 1<<63 - 1,
		})
	}
	for _, child := range append(n.Nodes, n.FloatingNodes...) {
		if err := walkSway(child, workspace, hasWS, out); err != nil {
			return err
		}
	}
	return nil
}

func (swayBackend) command(ctx context.Context, cmd string) error {
	c, err := sway.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.RunCommand(ctx, cmd)
}

func (b swayBackend) focus(ctx context.Context, id string) error {
	return b.command(ctx, "[con_id="+id+"] focus")
}

func (b swayBackend) close(ctx context.Context, id string) error {
	return b.command(ctx, "[con_id="+id+"] kill")
}
