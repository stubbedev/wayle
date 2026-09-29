package treeman

import (
	"bufio"
	"net"
)

// dial connects the daemon's unix socket.
func dial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

// readLines pumps NDJSON lines until the stream closes.
func readLines(conn net.Conn, out chan<- string) {
	defer close(out)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		out <- scanner.Text()
	}
	if scanner.Err() != nil {
		out <- ""
	}
}
