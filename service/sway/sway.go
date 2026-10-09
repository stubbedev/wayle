// Package sway is the sway/i3 IPC client: the i3 wire protocol over
// $SWAYSOCK, workspace snapshots, an event subscription, and layout
// commands.
package sway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SocketPath reads $SWAYSOCK; empty when sway is not running.
func SocketPath() string { return os.Getenv("SWAYSOCK") }

// message types (protocol.rs's MessageType).
const (
	msgRunCommand    = 0
	msgGetWorkspaces = 1
	msgSubscribe     = 2
	msgGetTree       = 4
	msgGetInputs     = 100
)

// eventBit marks a message as an event rather than a reply.
const eventBit = 0x80000000

// magic is the fixed i3-ipc header.
const magic = "i3-ipc"

// Workspace is one workspace snapshot (types.rs's WorkspaceReply).
type Workspace struct {
	ID      int64  `json:"id"`
	Num     int    `json:"num"`
	Name    string `json:"name"`
	Visible bool   `json:"visible"`
	Focused bool   `json:"focused"`
	Urgent  bool   `json:"urgent"`
	Output  string `json:"output"`
}

// writeFrame sends one message: magic, little-endian length, type,
// payload.
func writeFrame(conn net.Conn, msgType uint32, payload []byte) error {
	head := make([]byte, 14)
	copy(head, magic)
	binary.LittleEndian.PutUint32(head[6:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(head[10:], msgType)
	if _, err := conn.Write(head); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

// readFrame reads one message: validates the magic, returns the raw
// type and payload.
func readFrame(conn net.Conn) (uint32, []byte, error) {
	head := make([]byte, 14)
	if err := readFull(conn, head); err != nil {
		return 0, nil, err
	}
	if string(head[:6]) != magic {
		return 0, nil, errors.New("sway: bad magic in reply")
	}
	length := binary.LittleEndian.Uint32(head[6:])
	msgType := binary.LittleEndian.Uint32(head[10:])
	payload := make([]byte, length)
	if err := readFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}

func readFull(conn net.Conn, buf []byte) error {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return err
		}
		total += n
	}
	return nil
}

// Conn is one IPC connection. Requests serialize; events read on
// their own connection.
type Conn struct {
	mu   sync.Mutex
	conn net.Conn
}

// Connect dials $SWAYSOCK.
func Connect() (*Conn, error) {
	path := SocketPath()
	if path == "" {
		return nil, errors.New("sway: SWAYSOCK is not set")
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("sway: %w", err)
	}
	return &Conn{conn: conn}, nil
}

// Close drops the connection.
func (c *Conn) Close() error { return c.conn.Close() }

// request sends one message and returns the first reply's payload.
func (c *Conn) request(msgType uint32, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := writeFrame(c.conn, msgType, payload); err != nil {
		return nil, err
	}
	_, body, err := readFrame(c.conn)
	return body, err
}

// Workspaces lists the workspaces.
func (c *Conn) Workspaces() ([]Workspace, error) {
	body, err := c.request(msgGetWorkspaces, nil)
	if err != nil {
		return nil, err
	}
	var out []Workspace
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Input is one GET_INPUTS entry (types.rs's InputReply), the fields
// the keyboard layout reads.
type Input struct {
	Type                string  `json:"type"`
	XkbActiveLayoutName *string `json:"xkb_active_layout_name"`
}

// Inputs is GET_INPUTS.
func (c *Conn) Inputs() ([]Input, error) {
	body, err := c.request(msgGetInputs, nil)
	if err != nil {
		return nil, err
	}
	var out []Input
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// KeyboardLayout is refresh_keyboard_layout: the active layout of the
// first keyboard reporting one; "" when none does.
func KeyboardLayout(inputs []Input) string {
	for _, in := range inputs {
		if in.Type == "keyboard" && in.XkbActiveLayoutName != nil {
			return *in.XkbActiveLayoutName
		}
	}
	return ""
}

// RunCommand executes one sway command; the first error in the reply
// wins.
func (c *Conn) RunCommand(ctx context.Context, command string) error {
	body, err := c.request(msgRunCommand, []byte(command))
	if err != nil {
		return err
	}
	var results []struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return err
	}
	for _, r := range results {
		if !r.Success {
			return errors.New("sway: " + r.Error)
		}
	}
	return nil
}

// Quote is service.rs's quote: a double-quoted workspace name with
// embedded backslashes and quotes escaped.
func Quote(name string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `"`, `\"`) + `"`
}

// FocusCommand is focus_command for a workspace the snapshot knows:
// by number when it has one, else by quoted name.
func FocusCommand(ws Workspace) string {
	if ws.Num >= 0 {
		return "workspace --no-auto-back-and-forth number " + strconv.Itoa(ws.Num)
	}
	return "workspace --no-auto-back-and-forth " + Quote(ws.Name)
}

// FocusWorkspace focuses one workspace (focus:this).
func (c *Conn) FocusWorkspace(ctx context.Context, ws Workspace) error {
	return c.RunCommand(ctx, FocusCommand(ws))
}

// FocusNextOnOutput is focus:next.
func (c *Conn) FocusNextOnOutput(ctx context.Context) error {
	return c.RunCommand(ctx, "workspace next_on_output")
}

// FocusPrevOnOutput is focus:previous.
func (c *Conn) FocusPrevOnOutput(ctx context.Context) error {
	return c.RunCommand(ctx, "workspace prev_on_output")
}

// FocusBackAndForth is focus:last.
func (c *Conn) FocusBackAndForth(ctx context.Context) error {
	return c.RunCommand(ctx, "workspace back_and_forth")
}

// Window is one leaf window of the container tree (core/window.rs's
// snapshot), tagged with the enclosing workspace's id.
type Window struct {
	ID          int64
	Title       string
	HasTitle    bool
	AppID       string
	HasAppID    bool
	WorkspaceID int64
	// HasWorkspace is false for windows outside any workspace (the
	// scratchpad's __i3 container still counts as a workspace node).
	HasWorkspace bool
	Focused      bool
	Floating     bool
	Urgent       bool
}

// treeNode is types.rs's TreeNode.
type treeNode struct {
	ID               int64   `json:"id"`
	Type             string  `json:"type"`
	Name             *string `json:"name"`
	AppID            *string `json:"app_id"`
	Focused          bool    `json:"focused"`
	Urgent           bool    `json:"urgent"`
	WindowProperties *struct {
		Class *string `json:"class"`
	} `json:"window_properties"`
	Nodes         []treeNode `json:"nodes"`
	FloatingNodes []treeNode `json:"floating_nodes"`
}

// isWindow is TreeNode::is_window: a con/floating_con leaf.
func (n *treeNode) isWindow() bool {
	return (n.Type == "con" || n.Type == "floating_con") && len(n.Nodes) == 0 && len(n.FloatingNodes) == 0
}

// collectWindows is refresh.rs's collect_windows.
func collectWindows(n *treeNode, workspace int64, hasWorkspace bool, out *[]Window) {
	if n.Type == "workspace" {
		workspace, hasWorkspace = n.ID, true
	}
	if n.isWindow() {
		w := Window{
			ID: n.ID, WorkspaceID: workspace, HasWorkspace: hasWorkspace,
			Focused: n.Focused, Floating: n.Type == "floating_con", Urgent: n.Urgent,
		}
		if n.Name != nil {
			w.Title, w.HasTitle = *n.Name, true
		}
		// resolved_app_id: the Wayland app_id, else the X11 class.
		switch {
		case n.AppID != nil:
			w.AppID, w.HasAppID = *n.AppID, true
		case n.WindowProperties != nil && n.WindowProperties.Class != nil:
			w.AppID, w.HasAppID = *n.WindowProperties.Class, true
		}
		*out = append(*out, w)
		return
	}
	for i := range n.Nodes {
		collectWindows(&n.Nodes[i], workspace, hasWorkspace, out)
	}
	for i := range n.FloatingNodes {
		collectWindows(&n.FloatingNodes[i], workspace, hasWorkspace, out)
	}
}

// Tree returns the raw GET_TREE reply, for callers that walk fields
// Windows does not model (the launcher's window mode).
func (c *Conn) Tree() ([]byte, error) {
	return c.request(msgGetTree, nil)
}

// Windows walks GET_TREE into its leaf windows, tiling before
// floating per container, in tree order.
func (c *Conn) Windows() ([]Window, error) {
	body, err := c.request(msgGetTree, nil)
	if err != nil {
		return nil, err
	}
	var root treeNode
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	var out []Window
	collectWindows(&root, 0, false, &out)
	return out, nil
}

// Subscribe connects a second socket, subscribes to the workspace and
// window streams, and ticks per event; each tick is the cue to
// re-query. The channel closes when the stream ends; stop closes the
// socket.
func Subscribe() (<-chan struct{}, func(), error) {
	return SubscribeTo("workspace", "window")
}

// SubscribeTo is Subscribe over the named event streams.
func SubscribeTo(events ...string) (<-chan struct{}, func(), error) {
	path := SocketPath()
	if path == "" {
		return nil, nil, errors.New("sway: SWAYSOCK is not set")
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("sway: %w", err)
	}
	payload, err := json.Marshal(events)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if werr := writeFrame(conn, msgSubscribe, payload); werr != nil {
		_ = conn.Close()
		return nil, nil, werr
	}
	// The ack is an ordinary reply; skip any event racing ahead of it.
	for {
		msgType, body, rerr := readFrame(conn)
		if rerr != nil {
			_ = conn.Close()
			return nil, nil, rerr
		}
		if msgType&eventBit != 0 {
			continue
		}
		var ack struct {
			Success bool `json:"success"`
		}
		if json.Unmarshal(body, &ack) != nil || !ack.Success {
			_ = conn.Close()
			return nil, nil, errors.New("sway: subscribe rejected")
		}
		break
	}
	ticks := make(chan struct{}, 1)
	go func() {
		defer close(ticks)
		for {
			if _, _, err := readFrame(conn); err != nil {
				return
			}
			select {
			case ticks <- struct{}{}:
			default:
			}
		}
	}()
	var once sync.Once
	stop := func() { once.Do(func() { _ = conn.Close() }) }
	return ticks, stop, nil
}

// IsRunning reports whether a sway socket is advertised.
func IsRunning() bool { return SocketPath() != "" }
