// Package niri is the niri IPC client (wayle-niri): newline-delimited
// JSON requests and replies over $NIRI_SOCKET in niri-ipc's serde
// shapes, workspace and window snapshots, actions, and the event
// stream on a second socket.
package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// SocketEnv is niri_ipc::socket::SOCKET_PATH_ENV.
const SocketEnv = "NIRI_SOCKET"

// ErrNotRunning is Error::NiriNotRunning: $NIRI_SOCKET is unset.
var ErrNotRunning = errors.New("niri: NIRI_SOCKET is not set")

// SocketPath reads $NIRI_SOCKET; empty when niri is not running.
func SocketPath() string { return os.Getenv(SocketEnv) }

// IsRunning reports whether a niri socket is advertised.
func IsRunning() bool { return SocketPath() != "" }

// Workspace is niri_ipc::Workspace.
type Workspace struct {
	ID             uint64  `json:"id"`
	Idx            uint8   `json:"idx"`
	Name           *string `json:"name"`
	Output         *string `json:"output"`
	IsUrgent       bool    `json:"is_urgent"`
	IsActive       bool    `json:"is_active"`
	IsFocused      bool    `json:"is_focused"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}

// WindowLayout is the subset of niri_ipc::WindowLayout the bar reads.
type WindowLayout struct {
	// PosInScrollingLayout is the 1-based (column, tile) position;
	// nil for floating windows.
	PosInScrollingLayout *[2]int `json:"pos_in_scrolling_layout"`
}

// Window is niri_ipc::Window.
type Window struct {
	ID          uint64       `json:"id"`
	Title       *string      `json:"title"`
	AppID       *string      `json:"app_id"`
	PID         *int32       `json:"pid"`
	WorkspaceID *uint64      `json:"workspace_id"`
	IsFocused   bool         `json:"is_focused"`
	IsFloating  bool         `json:"is_floating"`
	IsUrgent    bool         `json:"is_urgent"`
	Layout      WindowLayout `json:"layout"`
}

// RejectedError is Error::NiriRejected: niri replied Err(message).
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return "niri rejected the request: " + e.Message }

// reply is Result<Response, String> as serde tags it.
type reply struct {
	Ok  json.RawMessage `json:"Ok"`
	Err *string         `json:"Err"`
}

// decodeReply unwraps one reply line into its Response.
func decodeReply(line []byte) (json.RawMessage, error) {
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, fmt.Errorf("niri: parse reply: %w", err)
	}
	if r.Err != nil {
		return nil, &RejectedError{Message: *r.Err}
	}
	if r.Ok == nil {
		return nil, errors.New("niri: reply carries neither Ok nor Err")
	}
	return r.Ok, nil
}

// expectHandled checks a Response is the unit Handled.
func expectHandled(resp json.RawMessage, request string) error {
	var s string
	if err := json.Unmarshal(resp, &s); err != nil || s != "Handled" {
		return fmt.Errorf("niri: unexpected response to %s", request)
	}
	return nil
}

// Conn is the persistent command socket; niri answers requests in
// order, so one write + one reply line runs under the mutex.
type Conn struct {
	mu     sync.Mutex
	conn   net.Conn
	reader *bufio.Reader
}

// Connect dials $NIRI_SOCKET.
func Connect() (*Conn, error) {
	path := SocketPath()
	if path == "" {
		return nil, ErrNotRunning
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("niri: command socket: %w", err)
	}
	return &Conn{conn: conn, reader: bufio.NewReader(conn)}, nil
}

// Close drops the connection.
func (c *Conn) Close() error { return c.conn.Close() }

// request sends one serialized Request and returns the Response.
func (c *Conn) request(req any) (json.RawMessage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, werr := c.conn.Write(append(body, '\n')); werr != nil {
		return nil, fmt.Errorf("niri: write: %w", werr)
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		if len(line) == 0 {
			return nil, errors.New("niri: command socket closed")
		}
		return nil, fmt.Errorf("niri: read: %w", err)
	}
	return decodeReply(line)
}

// Workspaces is Request::Workspaces.
func (c *Conn) Workspaces() ([]Workspace, error) {
	resp, err := c.request("Workspaces")
	if err != nil {
		return nil, err
	}
	var out struct {
		Workspaces *[]Workspace `json:"Workspaces"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Workspaces == nil {
		return nil, errors.New("niri: unexpected response to workspaces")
	}
	return *out.Workspaces, nil
}

// Windows is Request::Windows.
func (c *Conn) Windows() ([]Window, error) {
	resp, err := c.request("Windows")
	if err != nil {
		return nil, err
	}
	var out struct {
		Windows *[]Window `json:"Windows"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Windows == nil {
		return nil, errors.New("niri: unexpected response to windows")
	}
	return *out.Windows, nil
}

// KeyboardLayouts is niri_ipc::KeyboardLayouts: the configured layout
// names and the active one's index.
type KeyboardLayouts struct {
	Names      []string `json:"names"`
	CurrentIdx uint8    `json:"current_idx"`
}

// Current is the active layout's name; false when the index points
// past the names.
func (k KeyboardLayouts) Current() (string, bool) {
	if int(k.CurrentIdx) >= len(k.Names) {
		return "", false
	}
	return k.Names[k.CurrentIdx], true
}

// KeyboardLayouts is Request::KeyboardLayouts.
func (c *Conn) KeyboardLayouts() (KeyboardLayouts, error) {
	resp, err := c.request("KeyboardLayouts")
	if err != nil {
		return KeyboardLayouts{}, err
	}
	var out struct {
		KeyboardLayouts *KeyboardLayouts `json:"KeyboardLayouts"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.KeyboardLayouts == nil {
		return KeyboardLayouts{}, errors.New("niri: unexpected response to keyboard layouts")
	}
	return *out.KeyboardLayouts, nil
}

// Action dispatches one niri_ipc::Action, given as its serde form:
// the variant name and its (struct) payload.
func (c *Conn) Action(ctx context.Context, name string, payload any) error {
	if payload == nil {
		payload = struct{}{}
	}
	resp, err := c.request(map[string]any{"Action": map[string]any{name: payload}})
	if err != nil {
		return err
	}
	return expectHandled(resp, "action")
}

// FocusWorkspaceID is Action::FocusWorkspace with a
// WorkspaceReferenceArg::Id reference.
func (c *Conn) FocusWorkspaceID(ctx context.Context, id uint64) error {
	return c.Action(ctx, "FocusWorkspace", map[string]any{"reference": map[string]uint64{"Id": id}})
}

// FocusWorkspaceDown is Action::FocusWorkspaceDown (focus:next).
func (c *Conn) FocusWorkspaceDown(ctx context.Context) error {
	return c.Action(ctx, "FocusWorkspaceDown", nil)
}

// FocusWorkspaceUp is Action::FocusWorkspaceUp (focus:previous).
func (c *Conn) FocusWorkspaceUp(ctx context.Context) error {
	return c.Action(ctx, "FocusWorkspaceUp", nil)
}

// FocusWorkspacePrevious is Action::FocusWorkspacePrevious
// (focus:last).
func (c *Conn) FocusWorkspacePrevious(ctx context.Context) error {
	return c.Action(ctx, "FocusWorkspacePrevious", nil)
}

// Subscribe opens the event-stream socket, sends Request::EventStream,
// checks the Handled ack, and ticks once per event line; each tick is
// the cue to re-query. The channel closes when the stream ends; stop
// closes the socket.
func Subscribe() (<-chan struct{}, func(), error) {
	path := SocketPath()
	if path == "" {
		return nil, nil, ErrNotRunning
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("niri: event socket: %w", err)
	}
	if _, werr := conn.Write([]byte("\"EventStream\"\n")); werr != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("niri: event socket: %w", werr)
	}
	reader := bufio.NewReader(conn)
	ack, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return nil, nil, errors.New("niri: event stream closed before the handshake")
	}
	resp, err := decodeReply(ack)
	if err == nil {
		err = expectHandled(resp, "event-stream")
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	ticks := make(chan struct{}, 1)
	go func() {
		defer close(ticks)
		for {
			if _, err := reader.ReadBytes('\n'); err != nil {
				return
			}
			// Coalesce: one pending tick covers any burst, the
			// re-query reads the latest state.
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
