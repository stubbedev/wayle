// Package treeman is the treeman daemon client: worktree health as a
// status snapshot from the `treeman status` CLI, refreshed whenever
// the daemon's event socket signals activity.
package treeman

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
)

// Bucket is the coarse worktree state, mirroring treeman's bucket
// strings.
type Bucket int

// Buckets, most severe last for the comparison in WorstBucket.
const (
	BucketStable Bucket = iota
	BucketUp
	BucketDown
	BucketFailed
)

// Repo is one registered repo and its active worktrees.
type Repo struct {
	Repo      string `json:"repo"`
	Total     uint32 `json:"total"`
	Worktrees []struct {
		Branch string `json:"branch"`
		Slug   string `json:"slug"`
		State  string `json:"state"`
		Bucket string `json:"bucket"`
		IsMain bool   `json:"is_main"`
		Path   string `json:"path"`
	} `json:"worktrees"`
}

// Status is the aggregated worktree health across every registered
// repo (treeman status's JSON).
type Status struct {
	Total  uint32 `json:"total"`
	Stable uint32 `json:"stable"`
	Up     uint32 `json:"up"`
	Down   uint32 `json:"down"`
	Failed uint32 `json:"failed"`
	Class  string `json:"class"`
	Repos  []Repo `json:"repos"`
}

// WorstBucket is model.rs's worst_bucket: failed > down > up > stable.
func (s Status) WorstBucket() Bucket {
	switch {
	case s.Failed > 0:
		return BucketFailed
	case s.Down > 0:
		return BucketDown
	case s.Up > 0:
		return BucketUp
	}
	return BucketStable
}

// SocketPath mirrors treeman's own lookup order: $TREEMAN_SOCKET →
// $XDG_RUNTIME_DIR/treeman.sock → $XDG_DATA_HOME/treeman/treeman.sock
// → ~/.local/share/treeman/treeman.sock.
func SocketPath() (string, bool) {
	if p := os.Getenv("TREEMAN_SOCKET"); p != "" {
		return p, true
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "treeman.sock"), true
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", false
		}
		dataHome = filepath.Join(home, ".local/share")
	}
	return filepath.Join(dataHome, "treeman", "treeman.sock"), true
}

const subscribeRequest = `{"method":"event_subscribe","event_subscribe":{}}` + "\n"

// Source is the module's seam. Nil snapshots mean treeman is not
// installed.
type Source interface {
	// Read runs `treeman status` and parses the snapshot.
	Read(ctx context.Context) (*Status, error)
	// Subscribe ticks on daemon activity (debounced). The channel
	// closes when the source stops; stop terminates the loop.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

// System is the real client. Zero-value usable through New.
type System struct {
	binary string

	changes feed.Tick
	start   sync.Once
	halt    sync.Once
	stopped chan struct{}
}

// New builds the client for the given treeman binary.
func New(binary string) *System {
	if binary == "" {
		binary = "treeman"
	}
	return &System{binary: binary, stopped: make(chan struct{})}
}

// Read runs `treeman status`; a missing binary or non-zero exit reads
// as nil (the Rust service's Empty/Unavailable paths).
func (s *System) Read(ctx context.Context) (*Status, error) {
	out, err := exec.CommandContext(ctx, s.binary, "status").Output() //nolint:gosec // the binary name is the client's own
	if err != nil {
		return nil, nil
	}
	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, nil
	}
	return &status, nil
}

// Subscribe starts the event loop: an initial read, then a refetch
// whenever the event stream delivers (bursts collapse into one read
// after a short debounce). The loop reconnects when the socket drops.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	s.start.Do(func() { go s.run() })
	ticks, stop := s.changes.Subscribe()
	go func() {
		select {
		case <-ctx.Done():
		case <-s.stopped:
		}
		stop()
	}()
	return ticks, stop, nil
}

// run is the reconnect loop; the debounce mirrors the Rust service.
func (s *System) run() {
	stopped := s.stopped
	feed.Notify(&s.changes)
	for {
		select {
		case <-stopped:
			return
		default:
		}
		if s.consume(stopped) {
			return
		}
		select {
		case <-stopped:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// consume drains one event connection; true return means stop.
func (s *System) consume(stopped chan struct{}) bool {
	path, ok := SocketPath()
	if !ok {
		time.Sleep(2 * time.Second)
		return false
	}
	conn, err := dial(path)
	if err != nil {
		time.Sleep(2 * time.Second)
		return false
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(subscribeRequest)); err != nil {
		return false
	}
	lines := make(chan string, 8)
	go readLines(conn, lines)
	dirty := false
	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	for {
		select {
		case <-stopped:
			return true
		case _, ok := <-lines:
			if !ok {
				if dirty {
					feed.Notify(&s.changes)
				}
				return false
			}
			dirty = true
			debounce.Reset(300 * time.Millisecond)
		case <-debounce.C:
			feed.Notify(&s.changes)
			dirty = false
			debounce.Reset(time.Hour)
			debounce.Stop()
		}
	}
}

// Stop terminates the event loop and closes every subscriber.
func (s *System) Stop() {
	s.halt.Do(func() {
		close(s.stopped)
		s.changes.Close()
	})
}
