package hyprsunset

import (
	"errors"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// State is the running filter's live values.
type State struct {
	Temp  int
	Gamma int
}

// SocketPath is hyprsunset's control socket,
// $XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.hyprsunset.sock;
// false without either variable.
func SocketPath() (string, bool) {
	runtime, his := os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if runtime == "" || his == "" {
		return "", false
	}
	return filepath.Join(runtime, "hypr", his, ".hyprsunset.sock"), true
}

// QueryState asks a running hyprsunset for its temperature and gamma
// (helpers.rs query_state); false when none answers, which is how the
// module learns the filter is off.
func QueryState(socket string) (State, bool) {
	temp, ok := queryValue(socket, "temperature")
	if !ok {
		return State{}, false
	}
	gamma, ok := queryValue(socket, "gamma")
	if !ok {
		return State{}, false
	}
	return State{Temp: temp, Gamma: gamma}, true
}

// queryValue sends one command, half-closes, and reads the float reply
// rounded to an integer (parse_numeric_response).
func queryValue(socket, command string) (int, bool) {
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return 0, false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte(command)); err != nil {
		return 0, false
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if n == 0 || (err != nil && n == 0) {
		return 0, false
	}
	return parseNumeric(string(buf[:n]))
}

// parseNumeric reads a float reply and rounds it.
func parseNumeric(reply string) (int, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(reply), 32)
	if err != nil || v < 0 {
		return 0, false
	}
	return int(math.Round(v)), true
}

// Filter owns the hyprsunset child wayle spawned. One per process, like
// the Rust CHILD static: stopping signals exactly that child instead of
// every hyprsunset on the system.
type Filter struct {
	mu    sync.Mutex
	child *exec.Cmd
	// command builds the child; tests swap it.
	command func(temp, gamma int) *exec.Cmd
}

// NewFilter returns the driver for the real hyprsunset binary.
func NewFilter() *Filter {
	return &Filter{command: func(temp, gamma int) *exec.Cmd {
		return exec.Command("hyprsunset", "-t", strconv.Itoa(temp), "-g", strconv.Itoa(gamma)) //nolint:gosec // fixed binary, numeric args
	}}
}

// Start spawns hyprsunset at temp and gamma, terminating any previous
// child first so none leaks (helpers.rs start).
func (f *Filter) Start(temp, gamma int) error {
	child := f.command(temp, gamma)
	if err := child.Start(); err != nil {
		return err
	}
	f.mu.Lock()
	old := f.child
	f.child = child
	f.mu.Unlock()
	terminate(old)
	return nil
}

// Stop terminates the tracked child; a no-op without one.
func (f *Filter) Stop() error {
	f.mu.Lock()
	old := f.child
	f.child = nil
	f.mu.Unlock()
	terminate(old)
	return nil
}

// Running reports whether a child is tracked (tests).
func (f *Filter) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.child != nil
}

// terminate SIGTERMs a child - the signal hyprsunset's exit handler
// restores the gamma ramp on - and reaps it so no zombie lingers.
func terminate(child *exec.Cmd) {
	if child == nil || child.Process == nil {
		return
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return
	}
	go func() { _ = child.Wait() }()
}
