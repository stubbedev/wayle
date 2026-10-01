package launcheripc

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Session is one CLI session handed to the surface.
type Session struct {
	// ID is the connection's identity; ClientGone names it.
	ID      uint64
	Options SessionOptions
	// Replace is rofi -replace: displace a live session instead of
	// answering busy.
	Replace bool
	// Rows streams the dmenu rows; it closes on rows-done or when the
	// client goes away.
	Rows <-chan []string

	reply chan<- ServerFrame
	once  sync.Once
}

// Reply sends the session's terminal frame (result, cancelled, busy,
// or dump); the server writes it and closes the connection. Only the
// first call counts.
func (s *Session) Reply(f ServerFrame) {
	s.once.Do(func() { s.reply <- f })
}

// NewSession builds a session and the channel its terminal frame comes
// out of (one frame; a Release sends the zero frame). The server opens
// every session through it; a surface under test opens them directly.
func NewSession(id uint64, options SessionOptions, replace bool, rows <-chan []string) (*Session, <-chan ServerFrame) {
	reply := make(chan ServerFrame, 1)
	return &Session{ID: id, Options: options, Replace: replace, Rows: rows, reply: reply}, reply
}

// Release ends the session without a frame: the client is gone, so
// there is nobody to answer.
func (s *Session) Release() { s.Reply(ServerFrame{}) }

// Handler is the surface side of the socket.
type Handler interface {
	// Open starts a session. It is called on the connection's
	// goroutine and must not block; the surface eventually Replies.
	Open(s *Session)
	// ClientGone reports a CLI whose socket died (ctrl-C): close the
	// session if it is still the live one, and Reply to release the
	// connection.
	ClientGone(id uint64)
}

// Server accepts launcher sessions.
type Server struct {
	handler Handler
	ids     atomic.Uint64
}

// NewServer serves sessions to handler.
func NewServer(handler Handler) *Server { return &Server{handler: handler} }

// Listen binds the socket (removing a stale one, restricting it to the
// user) and accepts in the background. stop closes the listener and
// removes the socket file.
func (s *Server) Listen() (stop func(), err error) {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		log.Printf("launcher: could not restrict launcher socket permissions: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = ln.Close()
			_ = os.Remove(path)
		})
	}, nil
}

func writeFrame(conn net.Conn, f ServerFrame) {
	if f.Type == "" {
		return // Release: nothing to say
	}
	line, err := json.Marshal(f)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(line, '\n'))
}

// serve runs one connection: the mandatory open frame, the session,
// then the terminal frame and close.
func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return
	}
	var open ClientFrame
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &open); err != nil {
		log.Printf("launcher: malformed open frame: %v", err)
		return
	}
	if open.Type != FrameOpen {
		log.Printf("launcher: first frame was not open")
		return
	}
	options := SessionOptions{}
	if open.Options != nil {
		options = *open.Options
	}
	rows := make(chan []string, 16)
	sess, reply := NewSession(s.ids.Add(1), options, open.Replace, rows)
	s.handler.Open(sess)
	writeFrame(conn, ServerFrame{Type: FrameOpened})
	s.pump(sess, reader, rows, reply, conn)
}

// pump feeds client frames into the session until the terminal frame
// arrives or the client goes away.
func (s *Server) pump(sess *Session, reader *bufio.Reader, rows chan []string, reply <-chan ServerFrame, conn net.Conn) {
	lines := make(chan string)
	gone := make(chan struct{})
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(gone)
		for {
			line, err := reader.ReadString('\n')
			if strings.TrimSpace(line) != "" {
				select {
				case lines <- line:
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	rowsOpen := true
	closeRows := func() {
		if rowsOpen {
			close(rows)
			rowsOpen = false
		}
	}
	defer closeRows()
	for {
		select {
		case f := <-reply:
			writeFrame(conn, f)
			return
		case <-gone:
			// The client died: tear the surface down, then wait for
			// its reply so the surface is never left holding it.
			closeRows()
			s.handler.ClientGone(sess.ID)
			<-reply
			return
		case line := <-lines:
			var f ClientFrame
			if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &f); err != nil {
				log.Printf("launcher: malformed frame: %v", err)
				continue
			}
			switch f.Type {
			case FrameRows:
				if rowsOpen {
					select {
					case rows <- f.Items:
					case r := <-reply:
						writeFrame(conn, r)
						return
					}
				}
			case FrameRowsDone:
				closeRows()
			case FrameOpen:
				log.Printf("launcher: duplicate open ignored")
			}
		}
	}
}
