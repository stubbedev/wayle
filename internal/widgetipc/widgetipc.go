// Package widgetipc is the widget socket: the shell's JSON-RPC
// listener for out-of-process pushes (`wayle toast`, `wayle widget
// update`) and the client half those CLIs drive. It ports
// wayle-ipc/src/widget_socket.rs (protocol and client) and
// wayle-shell-core/src/services/widget_ipc.rs (the listener).
package widgetipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Methods.
const (
	MethodToastShow    = "toast.show"
	MethodWidgetUpdate = "widget.update"
)

// SocketPath mirrors wayle-ipc's lookup: $XDG_RUNTIME_DIR/wayle/
// widget.sock, falling back to /tmp/wayle-widget.sock.
func SocketPath() string {
	if dir, ok := os.LookupEnv("XDG_RUNTIME_DIR"); ok {
		return filepath.Join(dir, "wayle", "widget.sock")
	}
	return "/tmp/wayle-widget.sock"
}

// ToastRequest is one toast.show payload (ToastShowParams).
type ToastRequest struct {
	Label      *string  `json:"label,omitempty"`
	Icon       *string  `json:"icon,omitempty"`
	Percentage *float64 `json:"percentage,omitempty"`
	DurationMS *uint32  `json:"duration_ms,omitempty"`
	Preset     *string  `json:"preset,omitempty"`
	Class      *string  `json:"class,omitempty"`
}

// WidgetUpdate is one widget.update payload (WidgetUpdateParams): the
// output a custom widget receives as if its own command printed it.
type WidgetUpdate struct {
	ID     string `json:"id"`
	Output string `json:"output"`
}

// request is the JSON-RPC 2.0 envelope (Request); every field is
// required, as serde derives it.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      uint64          `json:"id"`
}

// response is the JSON-RPC reply (Response).
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      uint64          `json:"id"`
}

type rpcError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func okResponse(id uint64) response {
	return response{JSONRPC: "2.0", Result: json.RawMessage(`"ok"`), ID: id}
}

func errResponse(id uint64, code int64, msg string) response {
	return response{JSONRPC: "2.0", Error: &rpcError{Code: code, Message: msg}, ID: id}
}

// Server listens on the widget socket and publishes requests.
type Server struct {
	mu      sync.Mutex
	ln      net.Listener
	toasts  chan ToastRequest
	updates chan WidgetUpdate
	stopped chan struct{}
}

// NewServer builds the server; Listen binds the socket.
func NewServer() *Server {
	return &Server{
		toasts:  make(chan ToastRequest, 16),
		updates: make(chan WidgetUpdate, 16),
		stopped: make(chan struct{}),
	}
}

// Toasts ticks per toast.show request.
func (s *Server) Toasts() <-chan ToastRequest { return s.toasts }

// Updates ticks per widget.update request.
func (s *Server) Updates() <-chan WidgetUpdate { return s.updates }

// Listen binds the socket path (creating its directory, replacing a
// stale socket, restricting it to the owner) and starts accepting. The
// returned stop closes the listener and removes the socket file.
func (s *Server) Listen() (func(), error) {
	path := SocketPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	// A leftover socket from a previous run would make bind fail.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to bind widget socket at %s: %s", path, rustIOError(err))
	}
	_ = os.Chmod(path, 0o600)
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

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

// serve answers one connection's newline-delimited requests.
func (s *Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(conn)
	for scanner.Scan() {
		if err := enc.Encode(s.process(strings.TrimSpace(scanner.Text()))); err != nil {
			return
		}
	}
}

// process is widget_ipc.rs's process_request.
func (s *Server) process(line string) response {
	var req request
	if err := decodeStrict([]byte(line), &req, "jsonrpc", "method", "params", "id"); err != nil {
		return errResponse(0, -32700, "parse error: "+err.Error())
	}
	switch req.Method {
	case MethodWidgetUpdate:
		var params WidgetUpdate
		if err := decodeStrict(req.Params, &params, "id", "output"); err != nil {
			return errResponse(req.ID, -32602, "invalid params: "+err.Error())
		}
		select {
		case s.updates <- params:
		default:
		}
		return okResponse(req.ID)
	case MethodToastShow:
		var params ToastRequest
		if err := decodeStrict(req.Params, &params); err != nil {
			return errResponse(req.ID, -32602, "invalid params: "+err.Error())
		}
		select {
		case s.toasts <- params:
		default:
		}
		return okResponse(req.ID)
	default:
		return errResponse(req.ID, -32601, "unknown method: "+req.Method)
	}
}

// decodeStrict decodes a JSON object requiring the named fields, as a
// serde derive without defaults does ("missing field `id`").
func decodeStrict(raw []byte, into any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing field `%s`", name)
		}
	}
	return json.Unmarshal(raw, into)
}

// SendToast pushes one toast to the running shell (send_toast).
func SendToast(ctx context.Context, req ToastRequest) error {
	return send(ctx, MethodToastShow, req)
}

// SendWidgetUpdate pushes output to the widget whose config id is id
// (send_widget_update).
func SendWidgetUpdate(ctx context.Context, id, output string) error {
	return send(ctx, MethodWidgetUpdate, WidgetUpdate{ID: id, Output: output})
}

// ClientError is widget_socket.rs's ClientError, with its messages.
type ClientError struct {
	msg string
	err error
}

func (e *ClientError) Error() string { return e.msg }

func (e *ClientError) Unwrap() error { return e.err }

func send(ctx context.Context, method string, params any) error {
	path := SocketPath()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return &ClientError{msg: "cannot connect to wayle widget socket at " + path + ": " + rustIOError(err), err: err}
	}
	defer func() { _ = conn.Close() }()
	body, err := json.Marshal(params)
	if err != nil {
		return &ClientError{msg: "widget socket protocol error: " + err.Error(), err: err}
	}
	line, err := json.Marshal(request{JSONRPC: "2.0", Method: method, Params: body, ID: 1})
	if err != nil {
		return &ClientError{msg: "widget socket protocol error: " + err.Error(), err: err}
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return &ClientError{msg: "widget socket I/O failed: " + rustIOError(err), err: err}
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && reply == "" {
		return &ClientError{msg: "widget socket protocol error: EOF while parsing a value at line 1 column 0", err: err}
	}
	var resp response
	if err := decodeStrict([]byte(strings.TrimSpace(reply)), &resp, "jsonrpc", "id"); err != nil {
		return &ClientError{msg: "widget socket protocol error: " + err.Error(), err: err}
	}
	if resp.Error != nil {
		return &ClientError{msg: "widget update rejected: " + resp.Error.Message}
	}
	return nil
}

// rustIOError renders an OS error the way Rust's io::Error Display
// does: the C library's description and "(os error N)".
func rustIOError(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	desc := errno.Error()
	if desc != "" {
		desc = strings.ToUpper(desc[:1]) + desc[1:]
	}
	return fmt.Sprintf("%s (os error %d)", desc, int(errno))
}
