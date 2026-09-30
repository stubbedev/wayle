package launcheripc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

// ErrDisconnected is the shell closing the connection without a
// terminal frame.
var ErrDisconnected = errors.New("launcher session ended unexpectedly")

// Client is the CLI's connection to the launcher surface. The write
// half must stay open for the whole session: closing it half-closes
// the socket, and the shell reads that EOF as the client dying and
// tears the surface down.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

// Open connects the launcher socket and sends the open frame.
func Open(options SessionOptions, replace bool) (*Client, error) {
	path := SocketPath()
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to wayle launcher socket at %s: %w", path, err)
	}
	c := &Client{conn: conn, reader: bufio.NewReaderSize(conn, 64*1024)}
	if err := c.send(ClientFrame{Type: FrameOpen, Options: &options, Replace: replace}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// SendRows sends one chunk of dmenu rows.
func (c *Client) SendRows(items []string) error {
	return c.send(ClientFrame{Type: FrameRows, Items: items})
}

// FinishRows signals stdin EOF. It does not close the write half.
func (c *Client) FinishRows() error { return c.send(ClientFrame{Type: FrameRowsDone}) }

func (c *Client) send(f ClientFrame) error {
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("launcher socket protocol error: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("launcher socket I/O failed: %w", err)
	}
	return nil
}

// NextFrame reads the next shell frame; EOF is ErrDisconnected.
func (c *Client) NextFrame() (ServerFrame, error) {
	line, err := c.reader.ReadString('\n')
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
		return ServerFrame{}, ErrDisconnected
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return ServerFrame{}, fmt.Errorf("launcher socket I/O failed: %w", err)
	}
	var f ServerFrame
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &f); err != nil {
		return ServerFrame{}, fmt.Errorf("launcher socket protocol error: %w", err)
	}
	return f, nil
}

// Close closes the connection: the one EOF the shell should see.
func (c *Client) Close() error { return c.conn.Close() }
