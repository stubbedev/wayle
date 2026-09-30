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
)

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

// readFrame reads one reply: validates the magic, returns the type
// and payload.
func readFrame(conn net.Conn) ([]byte, error) {
	head := make([]byte, 14)
	if err := readFull(conn, head); err != nil {
		return nil, err
	}
	if string(head[:6]) != magic {
		return nil, errors.New("sway: bad magic in reply")
	}
	length := binary.LittleEndian.Uint32(head[6:])
	payload := make([]byte, length)
	if err := readFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
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
	body, err := readFrame(c.conn)
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

// SwitchTo focuses one workspace by name.
func (c *Conn) SwitchTo(ctx context.Context, name string) error {
	return c.RunCommand(ctx, "workspace "+strconv.Quote(name))
}

// Event is one workspace event payload.
type Event struct {
	Change string `json:"change"`
}

// Subscribe connects a second socket, subscribes to the workspace
// stream, and ticks per event. The returned stop closes the socket.
func Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	path := SocketPath()
	if path == "" {
		return nil, nil, errors.New("sway: SWAYSOCK is not set")
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, nil, err
	}
	if err := writeFrame(conn, msgSubscribe, []byte(`["workspace"]`)); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	// The subscribe reply arrives before the stream.
	if _, err := readFrame(conn); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	ticks := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		defer close(ticks)
		for {
			_, err := readFrame(conn)
			if err != nil {
				return
			}
			select {
			case ticks <- struct{}{}:
			default:
			}
		}
	}()
	stop := func() {
		close(done)
		_ = conn.Close()
	}
	go func() {
		<-done
	}()
	_ = ctx
	return ticks, stop, nil
}

// IsRunning reports whether a sway socket is reachable.
func IsRunning() bool { return SocketPath() != "" }
