// Package recorder is the screen recorder service: a state machine
// (idle/preparing/recording/paused) driving an external CLI engine,
// with the elapsed timer and the output-path building the Rust
// GStreamer service provides.
package recorder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Status is the recorder's coarse state.
type Status int

// Statuses.
const (
	StatusIdle Status = iota
	StatusStarting
	StatusRecording
	StatusStopping
)

// String names the status.
func (s Status) String() string {
	switch s {
	case StatusStarting:
		return "starting"
	case StatusRecording:
		return "recording"
	case StatusStopping:
		return "stopping"
	}
	return "idle"
}

// Options carries one recording's settings.
type Options struct {
	OutputPath  string
	FrameRate   int
	ShowCursor  bool
	Audio       bool
	AudioDevice string
}

// Handle is one running recording.
type Handle interface {
	// Pause and Resume toggle the capture; not-alive is not an error.
	Pause()
	Resume()
	// Stop finishes the file and waits for the engine to exit.
	Stop()
	// Done closes when the engine exited (with its error, if any).
	Done() <-chan error
}

// Engine spawns the recorder backend.
type Engine interface {
	Start(ctx context.Context, opts Options) (Handle, error)
}

// OutputPath builds a timestamped path in the configured (or default)
// directory (state.rs's output_path).
func OutputPath(configuredDir, format string) string {
	dir := configuredDir
	if dir == "" {
		dir = VideosDir()
	}
	name := fmt.Sprintf("wayle-%s.%s", time.Now().Format("20060102-150405"), format)
	return filepath.Join(dir, name)
}

// VideosDir resolves the default recordings directory:
// $XDG_VIDEOS_DIR or $HOME/Videos.
func VideosDir() string {
	if dir := os.Getenv("XDG_VIDEOS_DIR"); dir != "" {
		return dir
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, "Videos")
	}
	return "."
}

// Change is the state feed for the bar module.
type Change struct {
	Status      Status
	Active      bool
	Paused      bool
	Preparing   bool
	ElapsedSecs uint32
	OutputPath  string
}

// State is the recorder service. Safe for concurrent use.
type State struct {
	mu          sync.Mutex
	status      Status
	paused      bool
	elapsed     uint32
	output      string
	handle      Handle
	engine      Engine
	startDelay  time.Duration
	changes     chan Change
	timerStop   chan struct{}
	startCancel chan struct{}
}

// NewState builds the service over an engine with the portal-style
// start delay.
func NewState(engine Engine, startDelay time.Duration) *State {
	return &State{
		engine:     engine,
		startDelay: startDelay,
		changes:    make(chan Change, 8),
	}
}

// Changes ticks on every state flip and elapsed second.
func (s *State) Changes() <-chan Change { return s.changes }

// Snapshot reads the current state.
func (s *State) Snapshot() Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Change{
		Status:      s.status,
		Active:      s.status == StatusRecording,
		Paused:      s.paused,
		Preparing:   s.status == StatusStarting,
		ElapsedSecs: s.elapsed,
		OutputPath:  s.output,
	}
}

// notify drops a change, coalescing when pending.
func (s *State) notify() {
	snap := s.Snapshot()
	select {
	case s.changes <- snap:
	default:
	}
}

// Toggle starts when idle, stops otherwise (state.rs's toggle).
func (s *State) Toggle() { s.toggle() }

// toggle implements Toggle.
func (s *State) toggle() {
	s.mu.Lock()
	idle := s.status == StatusIdle
	s.mu.Unlock()
	if idle {
		s.Start()
	} else {
		s.Stop()
	}
}

// Start begins a recording with the given options builder. The
// Idle->Starting claim is synchronous, so rapid re-clicks are no-ops.
func (s *State) Start() { s.startWith() }

// startWith runs the start path. The Idle->Starting claim is
// synchronous (state.rs): a stop while the start is in flight cancels
// it, and the in-flight goroutine finishes the reset to Idle.
func (s *State) startWith() {
	s.mu.Lock()
	if s.status != StatusIdle {
		s.mu.Unlock()
		return
	}
	s.status = StatusStarting
	if s.startCancel != nil {
		close(s.startCancel)
	}
	cancel := make(chan struct{})
	s.startCancel = cancel
	engine := s.engine
	delay := s.startDelay
	s.mu.Unlock()
	s.notify()

	go func() {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-cancel:
				timer.Stop()
				s.finishStoppedStart()
				return
			}
		}
		output := OutputPath("", "mkv")
		s.mu.Lock()
		s.output = output
		s.mu.Unlock()
		handle, err := engine.Start(context.Background(), Options{
			OutputPath: output,
			FrameRate:  60,
			ShowCursor: true,
		})
		if err != nil {
			s.finishStoppedStart()
			return
		}
		select {
		case <-cancel:
			handle.Stop()
			s.finishStoppedStart()
			return
		default:
		}
		s.mu.Lock()
		if s.status != StatusStarting {
			s.mu.Unlock()
			handle.Stop()
			s.finishStoppedStart()
			return
		}
		s.handle = handle
		s.status = StatusRecording
		s.startTimerLocked()
		s.mu.Unlock()
		s.notify()
		go func() {
			<-handle.Done()
			s.engineDied(handle)
		}()
	}()
}

// finishStoppedStart completes a cancelled start: back to idle unless
// someone else moved the state on.
func (s *State) finishStoppedStart() {
	s.mu.Lock()
	if s.status != StatusIdle {
		s.resetIdleLocked()
	}
	s.mu.Unlock()
	s.notify()
}

// engineDied tears the recording down when the engine exited on its
// own (a crash, or the file finalized under an explicit Stop we
// already handled).
func (s *State) engineDied(h Handle) {
	s.mu.Lock()
	if s.status != StatusRecording || s.handle != h {
		s.mu.Unlock()
		return
	}
	s.resetIdleLocked()
	s.mu.Unlock()
	s.notify()
}

// Stop tears the recording down (state.rs's stop). From Starting it
// only cancels and flags Stopping; the in-flight start finishes the
// reset to Idle.
func (s *State) Stop() {
	s.mu.Lock()
	switch s.status {
	case StatusIdle:
		s.mu.Unlock()
		return
	case StatusStarting:
		if s.startCancel != nil {
			close(s.startCancel)
			s.startCancel = nil
		}
		s.status = StatusStopping
		s.mu.Unlock()
		s.notify()
		return
	case StatusStopping:
		s.mu.Unlock()
		return
	}
	handle := s.handle
	s.resetIdleLocked()
	s.mu.Unlock()
	if handle != nil {
		handle.Stop()
	}
	s.notify()
}

// SetPaused pauses or resumes a live recording.
func (s *State) SetPaused(paused bool) {
	s.mu.Lock()
	if s.status != StatusRecording || s.paused == paused {
		s.mu.Unlock()
		return
	}
	handle := s.handle
	s.paused = paused
	s.mu.Unlock()
	if handle != nil {
		if paused {
			handle.Pause()
		} else {
			handle.Resume()
		}
	}
	s.notify()
}

// resetIdleLocked returns to the idle state. The caller holds mu.
func (s *State) resetIdleLocked() {
	if s.timerStop != nil {
		close(s.timerStop)
		s.timerStop = nil
	}
	s.status = StatusIdle
	s.paused = false
	s.elapsed = 0
	s.output = ""
	s.handle = nil
}

// startTimerLocked runs the one-second elapsed ticker. The caller
// holds mu.
func (s *State) startTimerLocked() {
	stop := make(chan struct{})
	s.timerStop = stop
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.timerStop != stop || s.status != StatusRecording {
					s.mu.Unlock()
					return
				}
				s.elapsed++
				s.mu.Unlock()
				s.notify()
			}
		}
	}()
}

// FormatElapsed is helpers.rs's format_duration twin: H:MM:SS above
// the hour, M:SS within it.
func FormatElapsed(totalSecs uint32) string {
	hours := totalSecs / 3600
	minutes := (totalSecs % 3600) / 60
	seconds := totalSecs % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// WfRecorder is the wf-recorder CLI engine.
type WfRecorder struct{}

// handle is one wf-recorder process.
type handle struct {
	cmd  *exec.Cmd
	done chan error
}

// Start spawns wf-recorder with the mapped flags.
func (WfRecorder) Start(ctx context.Context, opts Options) (Handle, error) {
	args := []string{"-f", opts.OutputPath, "-r", strconv.Itoa(opts.FrameRate)}
	if opts.ShowCursor {
		args = append(args, "--cursor")
	}
	if opts.Audio {
		args = append(args, "--audio")
		if opts.AudioDevice != "" {
			args = append(args, "--audio-device", opts.AudioDevice)
		}
	}
	cmd := exec.CommandContext(ctx, "wf-recorder", args...) //nolint:gosec // a fixed binary name with validated flags
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("recorder: wf-recorder: %w", err)
	}
	h := &handle{cmd: cmd, done: make(chan error, 1)}
	go func() {
		h.done <- cmd.Wait()
	}()
	return h, nil
}

// Pause sends SIGUSR1 (wf-recorder's pause).
func (h *handle) Pause() { signal(h.cmd.Process, syscallSIGUSR1) }

// Resume sends SIGUSR2.
func (h *handle) Resume() { signal(h.cmd.Process, syscallSIGUSR2) }

// Stop sends SIGINT, which makes wf-recorder finalize the file.
func (h *handle) Stop() { signal(h.cmd.Process, syscallSIGINT) }

// Done closes when the engine exited.
func (h *handle) Done() <-chan error { return h.done }
