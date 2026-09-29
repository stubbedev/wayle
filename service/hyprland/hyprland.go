// Package hyprland speaks Hyprland's IPC: the command socket for
// queries and dispatches, and the event socket for the live stream.
// The wire formats match crates/wayle-hyprland: one request per
// connection on the command socket, answered up to an EOT byte (0x04),
// and newline-terminated "event>>payload" lines on the event socket.
package hyprland

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// endOfTransmission terminates every command-socket reply.
const endOfTransmission = '\x04'

// Paths resolves the IPC socket paths for the running Hyprland
// instance. Both HYPRLAND_INSTANCE_SIGNATURE and XDG_RUNTIME_DIR must
// be set; without them Hyprland is not running as far as wayle is
// concerned.
func Paths() (command, events string, err error) {
	his := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if his == "" {
		return "", "", errors.New("hyprland: HYPRLAND_INSTANCE_SIGNATURE is not set")
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return "", "", errors.New("hyprland: XDG_RUNTIME_DIR is not set")
	}
	base := filepath.Join(runtime, "hypr", his)
	return filepath.Join(base, ".socket.sock"), filepath.Join(base, ".socket2.sock"), nil
}

// Workspace is one entry of j/workspaces.
type Workspace struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Monitor     string `json:"monitor"`
	MonitorID   int    `json:"monitorID"`
	Windows     int    `json:"windows"`
	Fullscreen  bool   `json:"hasfullscreen"`
	Persistent  bool   `json:"ispersistent"`
	LastWindow  string `json:"lastwindow"`
	LastTitle   string `json:"lastwindowtitle"`
	TiledLayout string `json:"tiled_layout"`
}

// Monitor is the subset of j/monitors the shell consumes.
type Monitor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// ActiveWS is the workspace shown on the monitor.
	ActiveWS struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"activeWorkspace"`
	// Focused is the compositor's keyboard focus.
	Focused bool `json:"focused"`
}

// Connection is a live handle to one Hyprland instance.
type Connection struct {
	commandPath string
	eventPath   string
}

// Connect resolves the sockets for the running instance. No connection
// is opened yet: requests are one-connection-per-request, and events
// subscribe through Events.
func Connect() (*Connection, error) {
	command, events, err := Paths()
	if err != nil {
		return nil, err
	}
	return ConnectTo(command, events), nil
}

// ConnectTo builds a connection to explicit socket paths — the test
// seam and the way for embedders that already resolved the paths.
func ConnectTo(commandPath, eventPath string) *Connection {
	return &Connection{commandPath: commandPath, eventPath: eventPath}
}

// Command sends one request and returns the reply up to the EOT byte.
func (c *Connection) Command(request string) (string, error) {
	conn, err := net.Dial("unix", c.commandPath)
	if err != nil {
		return "", fmt.Errorf("hyprland: command socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(request)); err != nil {
		return "", fmt.Errorf("hyprland: write %q: %w", request, err)
	}
	reply, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("hyprland: read %q: %w", request, err)
	}
	return strings.TrimSuffix(string(reply), string(endOfTransmission)), nil
}

// Workspaces lists the instance's workspaces (j/workspaces).
func (c *Connection) Workspaces() ([]Workspace, error) {
	reply, err := c.Command("j/workspaces")
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]Workspace](reply)
}

// Monitors lists the instance's monitors (j/monitors).
func (c *Connection) Monitors() ([]Monitor, error) {
	reply, err := c.Command("j/monitors")
	if err != nil {
		return nil, err
	}
	return decodeJSON[[]Monitor](reply)
}

// Dispatch runs a dispatcher command ("workspace 3") and returns the
// compositor's ack.
func (c *Connection) Dispatch(command string) (string, error) {
	return c.Command("dispatch " + command)
}
