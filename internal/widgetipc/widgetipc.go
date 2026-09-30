// Package widgetipc is the widget socket: the shell's JSON-RPC
// listener for out-of-process pushes (`wayle toast`, custom widget
// updates) and the client half those CLIs drive.
package widgetipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Methods.
const (
	MethodToastShow    = "toast.show"
	MethodWidgetUpdate = "widget.update"
)

// SocketPath mirrors wayle-ipc's lookup: $XDG_RUNTIME_DIR/wayle/
// widget.sock, falling back to /tmp/wayle-widget.sock.
func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "wayle", "widget.sock")
	}
	return "/tmp/wayle-widget.sock"
}

// ToastRequest is one toast.show payload (widget_ipc.rs's ToastRequest).
type ToastRequest struct {
	Label      *string  `json:"label,omitempty"`
	Icon       *string  `json:"icon,omitempty"`
	Percentage *float64 `json:"percentage,omitempty"`
	DurationMS *uint32  `json:"duration_ms,omitempty"`
	Preset     *string  `json:"preset,omitempty"`
	Class      *string  `json:"class,omitempty"`
}

// request is the JSON-RPC 2.0 envelope.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is the JSON-RPC 2.0 reply.
type response struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      any        `json:"id"`
	Result  any        `json:"result,omitempty"`
	Error   *respError `json:"error,omitempty"`
}

type respError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server listens on the widget socket and publishes requests.
type Server struct {
	mu      sync.Mutex
	ln      net.Listener
	toasts  chan ToastRequest
	updates chan json.RawMessage
	stopped chan struct{}
}

// NewServer builds the server; Listen binds the socket.
func NewServer() *Server {
	return &Server{
		toasts:  make(chan ToastRequest, 16),
		updates: make(chan json.RawMessage, 16),
		stopped: make(chan struct{}),
	}
}

// Toasts ticks per toast.show request.
func (s *Server) Toasts() <-chan ToastRequest { return s.toasts }

// Updates ticks per widget.update request (raw params; the custom
// modules own their payload shape).
func (s *Server) Updates() <-chan json.RawMessage { return s.updates }

// Listen binds the socket path and starts accepting. The returned
// stop closes the listener and removes the socket file.
func (s *Server) Listen() (func(), error) {
	path := SocketPath()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// A stale socket from a crashed shell would fail the bind.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go s.accept()
	stop := func() {
		select {
		case <-s.stopped:
			return
		default:
		}
		close(s.stopped)
		_ = ln.Close()
		_ = os.Remove(path)
	}
	return stop, nil
}

// accept serves connections until the listener closes.
func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

// serve drains one connection's JSON lines.
func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		s.dispatch(conn, &req)
	}
}

// dispatch routes one request and replies.
func (s *Server) dispatch(conn net.Conn, req *request) {
	switch req.Method {
	case MethodToastShow:
		var params ToastRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			_ = json.NewEncoder(conn).Encode(response{JSONRPC: "2.0", ID: decodeID(req.ID), Error: &respError{Code: -32602, Message: err.Error()}})
			return
		}
		select {
		case s.toasts <- params:
		default:
		}
		_ = json.NewEncoder(conn).Encode(response{JSONRPC: "2.0", ID: decodeID(req.ID), Result: map[string]bool{"ok": true}})
	case MethodWidgetUpdate:
		select {
		case s.updates <- req.Params:
		default:
		}
		_ = json.NewEncoder(conn).Encode(response{JSONRPC: "2.0", ID: decodeID(req.ID), Result: map[string]bool{"ok": true}})
	default:
		_ = json.NewEncoder(conn).Encode(response{JSONRPC: "2.0", ID: decodeID(req.ID), Error: &respError{Code: -32601, Message: "unknown method " + req.Method}})
	}
}

// decodeID passes the request id back through any.
func decodeID(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var id any
	if err := json.Unmarshal(raw, &id); err != nil {
		return nil
	}
	return id
}

// SendToast pushes one toast to the running shell.
func SendToast(ctx context.Context, req ToastRequest) error {
	conn, err := Dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	payload, err := json.Marshal(request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  MethodToastShow,
		Params:  mustMarshal(req),
	})
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	var resp response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return nil
}

// RPCError is the daemon's JSON-RPC error reply.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return e.Message }

// Dial connects the widget socket.
func Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", SocketPath())
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func mustMarshal(v any) json.RawMessage {
	body, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return body
}
