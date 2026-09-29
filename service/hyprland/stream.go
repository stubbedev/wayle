package hyprland

import (
	"bufio"
	"context"
	"net"
)

// Events subscribes to the event socket and returns a stream of parsed
// events. The returned channel closes when the context is canceled or
// the compositor closes the socket. Lines that fail to parse are
// dropped, matching the Rust
// shell's warn-and-drop.
func (c *Connection) Events(ctx context.Context) (<-chan Event, error) {
	conn, err := net.Dial("unix", c.eventPath)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 64)
	go func() {
		defer func() { _ = conn.Close() }()
		defer close(out)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		notify := ctx.Done()
		for {
			select {
			case <-notify:
				return
			default:
			}
			if !scanner.Scan() {
				return
			}
			ev, ok := ParseEvent(scanner.Text())
			if !ok {
				continue
			}
			select {
			case out <- ev:
			case <-notify:
				return
			}
		}
	}()
	return out, nil
}
