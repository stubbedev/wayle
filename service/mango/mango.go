// Package mango is the MangoWM IPC client (wayle-mango): one-shot
// "dispatch <command>" requests and the "watch all-monitors" /
// "watch all-clients" streams, each line a full-state JSON frame.
// Mango is dwm-derived: every monitor carries a fixed set of tags.
package mango

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// SocketEnv is constants.rs's SOCKET_PATH_ENV.
const SocketEnv = "MANGO_INSTANCE_SIGNATURE"

// The watch subscriptions.
const (
	watchMonitors = "watch all-monitors"
	watchClients  = "watch all-clients"
)

// ErrNotRunning is Error::MangoNotRunning.
var ErrNotRunning = errors.New("mango: MANGO_INSTANCE_SIGNATURE is not set")

// SocketPath reads the socket path; empty when mango is not running.
func SocketPath() string { return os.Getenv(SocketEnv) }

// IsRunning reports whether a mango socket is advertised.
func IsRunning() bool { return SocketPath() != "" }

// Tag is types.rs's Tag.
type Tag struct {
	Index       uint32 `json:"index"`
	IsActive    bool   `json:"is_active"`
	IsUrgent    bool   `json:"is_urgent"`
	Layout      string `json:"layout"`
	ClientCount uint32 `json:"client_count"`
}

// Monitor is core/monitor.rs's Monitor.
type Monitor struct {
	Name       string   `json:"name"`
	IsActive   bool     `json:"active"`
	Tags       []Tag    `json:"tags"`
	ActiveTags []uint32 `json:"active_tags"`
}

// Client is core/client.rs's Client; empty title/app id read as
// unset, as from_snapshot filters them.
type Client struct {
	ID        uint32
	Title     string
	HasTitle  bool
	AppID     string
	HasAppID  bool
	Monitor   string
	Tags      []uint32
	IsUrgent  bool
	IsFocused bool
}

type clientSnapshot struct {
	ID        uint32   `json:"id"`
	Title     *string  `json:"title"`
	AppID     *string  `json:"appid"`
	Monitor   string   `json:"monitor"`
	Tags      []uint32 `json:"tags"`
	IsUrgent  bool     `json:"is_urgent"`
	IsFocused bool     `json:"is_focused"`
}

// RejectedError is Error::MangoRejected.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return "mango rejected the request: " + e.Message }

func dial() (net.Conn, error) {
	path := SocketPath()
	if path == "" {
		return nil, ErrNotRunning
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mango: %w", err)
	}
	return conn, nil
}

// request sends one command line on a fresh connection and returns
// the JSON reply; an "error" field is the rejection.
func request(command string) (map[string]any, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if _, werr := conn.Write([]byte(command + "\n")); werr != nil {
		return nil, fmt.Errorf("mango: write: %w", werr)
	}
	line, rerr := bufio.NewReader(conn).ReadBytes('\n')
	if len(line) == 0 {
		if rerr == nil {
			rerr = errors.New("empty reply")
		}
		return nil, fmt.Errorf("mango: command socket closed: %w", rerr)
	}
	var reply map[string]any
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, fmt.Errorf("mango: parse reply: %w", err)
	}
	if msg, ok := reply["error"].(string); ok {
		return nil, &RejectedError{Message: msg}
	}
	return reply, nil
}

// Dispatch runs one mango dispatcher.
func Dispatch(command string) error {
	_, err := request("dispatch " + command)
	return err
}

// ViewTag is view_tag: "view,<index>".
func ViewTag(index uint32) error { return Dispatch("view," + strconv.FormatUint(uint64(index), 10)) }

// ViewLeft is view_left (focus:previous).
func ViewLeft() error { return Dispatch("viewtoleft") }

// ViewRight is view_right (focus:next).
func ViewRight() error { return Dispatch("viewtoright") }

// Watcher holds the latest monitor and client frames from the two
// watch streams and ticks after each applied frame.
type Watcher struct {
	mu       sync.Mutex
	monitors []Monitor
	clients  []Client
	// gotMonitors/gotClients record a first applied frame per stream.
	gotMonitors, gotClients bool
	conns                   []net.Conn
	ticks                   chan struct{}
	once                    sync.Once
}

// Watch connects both watch streams (start_monitoring).
func Watch() (*Watcher, error) {
	w := &Watcher{ticks: make(chan struct{}, 1)}
	var wg sync.WaitGroup
	for _, sub := range []string{watchMonitors, watchClients} {
		conn, err := dial()
		if err != nil {
			w.Close()
			return nil, err
		}
		if _, werr := conn.Write([]byte(sub + "\n")); werr != nil {
			_ = conn.Close()
			w.Close()
			return nil, fmt.Errorf("mango: %s: %w", sub, werr)
		}
		w.conns = append(w.conns, conn)
		wg.Add(1)
		go w.pump(conn, sub, &wg)
	}
	go func() {
		wg.Wait()
		close(w.ticks)
	}()
	return w, nil
}

// pump applies each frame line of one stream.
func (w *Watcher) pump(conn net.Conn, sub string, wg *sync.WaitGroup) {
	defer wg.Done()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if applyErr := w.apply(sub, line); applyErr != nil {
				log.Printf("mango: %s: %v", sub, applyErr)
			} else {
				select {
				case w.ticks <- struct{}{}:
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// apply parses one frame; a frame that does not parse leaves the
// previous state in place (the Rust warn-and-continue).
func (w *Watcher) apply(sub string, line []byte) error {
	switch sub {
	case watchMonitors:
		var frame struct {
			Monitors []Monitor `json:"monitors"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("cannot parse monitor frame: %w", err)
		}
		w.mu.Lock()
		w.monitors, w.gotMonitors = frame.Monitors, true
		w.mu.Unlock()
	case watchClients:
		var frame struct {
			Clients []clientSnapshot `json:"clients"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("cannot parse client frame: %w", err)
		}
		clients := make([]Client, 0, len(frame.Clients))
		for _, s := range frame.Clients {
			c := Client{ID: s.ID, Monitor: s.Monitor, Tags: s.Tags, IsUrgent: s.IsUrgent, IsFocused: s.IsFocused}
			if s.Title != nil && *s.Title != "" {
				c.Title, c.HasTitle = *s.Title, true
			}
			if s.AppID != nil && *s.AppID != "" {
				c.AppID, c.HasAppID = *s.AppID, true
			}
			clients = append(clients, c)
		}
		w.mu.Lock()
		w.clients, w.gotClients = clients, true
		w.mu.Unlock()
	}
	return nil
}

// State returns copies of the latest monitors and clients.
func (w *Watcher) State() ([]Monitor, []Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Monitor(nil), w.monitors...), append([]Client(nil), w.clients...)
}

// Ready reports whether both streams have delivered a frame.
func (w *Watcher) Ready() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gotMonitors && w.gotClients
}

// Ticks fires after each applied frame; it closes when both streams
// end.
func (w *Watcher) Ticks() <-chan struct{} { return w.ticks }

// Close drops both streams.
func (w *Watcher) Close() {
	w.once.Do(func() {
		for _, conn := range w.conns {
			_ = conn.Close()
		}
	})
}
