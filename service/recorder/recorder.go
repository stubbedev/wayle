// Package recorder is the screen recorder service (wayle-shell-core
// services/recorder and the wayle-recorder engine): the lifecycle state
// machine (idle, starting, recording, stopping), the elapsed timer,
// options from the [modules.recorder] config, and an engine that
// negotiates the ScreenCast portal and records through GStreamer.
package recorder

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/feed"
)

// Status is the lifecycle gate (state.rs's Status), updated
// synchronously so rapid clicks never double-start.
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

// Toast and notification constants (state.rs).
const (
	toastIcon      = "ld-circle-dot-symbolic"
	errorIcon      = "ld-alert-triangle-symbolic"
	savedIcon      = "ld-video-symbolic"
	startToastMS   = 1000
	stopToastMS    = 1500
	notifyAppName  = "Wayle"
	elapsedTick    = time.Second
	outputDirPerms = 0o755
)

// Session is a negotiated capture source, held across the start delay
// and closed when the recording ends.
type Session interface {
	Cast() Cast
	Close()
}

// Recording is one running capture.
type Recording interface {
	// SetPaused pauses or resumes the capture.
	SetPaused(paused bool) error
	// Stop finalizes the file, blocking until the muxer is done.
	Stop()
}

// Engine opens capture sessions and records them (wayle-recorder's
// Recorder: open_session, start).
type Engine interface {
	// OpenSession negotiates the source, showing the picker when no
	// grant is cached; ctx cancels it.
	OpenSession(ctx context.Context, showCursor bool) (Session, error)
	// Start records session with opts; term reports an unexpected end
	// (a source disconnect, a full disk), never a requested Stop.
	Start(session Session, opts Options, term func(reason string)) (Recording, error)
}

// Hooks are the user-facing reports: an OSD toast and a desktop
// notification. Nil hooks are skipped.
type Hooks struct {
	Toast  func(label, icon string, durationMS uint32)
	Notify func(appName, summary, body, icon string)
}

// Change is the state the bar module, the dropdown, and the daemon
// read.
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
	engine Engine
	config func() config.RecorderConfig
	hooks  Hooks

	mu          sync.Mutex
	status      Status
	active      bool
	paused      bool
	preparing   bool
	elapsed     uint32
	output      string
	recording   Recording
	session     Session
	cancelStart context.CancelFunc
	// gen numbers each start, so a late termination report from an
	// earlier recording is ignored.
	gen       uint64
	timerStop chan struct{}
	changes   *feed.Feed[Change]
}

// NewState builds the service over an engine, reading options from
// cfg at each start.
func NewState(engine Engine, cfg func() config.RecorderConfig, hooks Hooks) *State {
	return &State{engine: engine, config: cfg, hooks: hooks, changes: feed.New[Change](8)}
}

// Changes delivers a snapshot on every change and elapsed second to
// every subscriber; stop ends the feed.
func (s *State) Changes() (<-chan Change, func()) { return s.changes.Subscribe() }

// Snapshot reads the current state.
func (s *State) Snapshot() Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *State) snapshotLocked() Change {
	return Change{
		Status:      s.status,
		Active:      s.active,
		Paused:      s.paused,
		Preparing:   s.preparing,
		ElapsedSecs: s.elapsed,
		OutputPath:  s.output,
	}
}

func (s *State) notify() { s.changes.Publish(s.Snapshot()) }

// Toggle keys off the lifecycle, not active: during the start delay a
// toggle cancels the pending start (state.rs's toggle).
func (s *State) Toggle() {
	s.mu.Lock()
	idle := s.status == StatusIdle
	s.mu.Unlock()
	if idle {
		s.Start()
	} else {
		s.Stop()
	}
}

// Start begins a recording with the current config. The Idle→Starting
// claim is synchronous, so a second call while one is in flight is a
// no-op; the rest (portal, delay, pipeline) runs off the caller.
func (s *State) Start() {
	s.mu.Lock()
	if s.status != StatusIdle {
		s.mu.Unlock()
		return
	}
	s.status = StatusStarting
	s.preparing = true
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelStart = cancel
	s.gen++
	gen := s.gen
	s.mu.Unlock()
	s.notify()
	go s.runStart(ctx, gen)
}

// runStart is run_start: the output directory, the portal session
// (first, so the picker answers the click), the announced start delay,
// then the pipeline.
func (s *State) runStart(ctx context.Context, gen uint64) {
	cfg := s.config()
	opts := BuildOptions(cfg, time.Now())
	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), outputDirPerms); err != nil {
		s.failStart(err)
		return
	}
	session, err := s.engine.OpenSession(ctx, opts.ShowCursor)
	if ctx.Err() != nil {
		if session != nil {
			session.Close()
		}
		s.resetIdle()
		return
	}
	if err != nil {
		s.failStart(err)
		return
	}
	s.toast(i18n.T("recorder-toast-starting"), toastIcon, startToastMS)
	select {
	case <-ctx.Done():
		session.Close()
		s.resetIdle()
		return
	case <-time.After(time.Duration(cfg.StartDelayMs) * time.Millisecond):
	}
	rec, err := s.engine.Start(session, opts, func(reason string) { s.unexpectedStop(gen, reason) })
	if err != nil {
		session.Close()
		s.failStart(err)
		return
	}
	s.commit(ctx, rec, session, opts.OutputPath)
}

// commit is commit_recording: Recording, unless a stop cancelled the
// start meanwhile, which tears the fresh pipeline back down.
func (s *State) commit(ctx context.Context, rec Recording, session Session, path string) {
	s.mu.Lock()
	if s.status != StatusStarting || ctx.Err() != nil {
		s.mu.Unlock()
		rec.Stop()
		session.Close()
		s.resetIdle()
		return
	}
	s.status = StatusRecording
	s.recording, s.session = rec, session
	s.output = path
	s.elapsed = 0
	s.paused = false
	s.active = true
	s.preparing = false
	s.startTimerLocked()
	s.mu.Unlock()
	s.notify()
}

// failStart is fail_start.
func (s *State) failStart(err error) {
	s.resetIdle()
	log.Printf("recorder: failed to start recording: %v", err)
	s.showError(fmt.Sprintf("%s: %v", i18n.T("recorder-toast-failed"), err))
}

// unexpectedStop is handle_unexpected_stop: a live recording that died
// on its own is torn down and reported.
func (s *State) unexpectedStop(gen uint64, reason string) {
	s.mu.Lock()
	if s.status != StatusRecording || s.gen != gen {
		s.mu.Unlock()
		return
	}
	s.status = StatusStopping
	rec, session := s.recording, s.session
	s.mu.Unlock()
	log.Printf("recorder: recording terminated unexpectedly: %s", reason)
	rec.Stop()
	session.Close()
	s.resetIdle()
	s.showError(fmt.Sprintf("%s: %s", i18n.T("recorder-toast-failed"), reason))
}

// Stop stops the recording, or cancels one still starting. Only the
// first call from Starting or Recording does work. From Recording the
// UI flips to stopped at once, the blocking finalize runs off the
// caller, and the status settles to Idle after it.
func (s *State) Stop() {
	s.mu.Lock()
	prev := s.status
	if prev == StatusIdle || prev == StatusStopping {
		s.mu.Unlock()
		return
	}
	s.status = StatusStopping
	if s.cancelStart != nil {
		s.cancelStart()
	}
	if prev == StatusStarting {
		// The in-flight start sees the cancellation and resets.
		s.mu.Unlock()
		s.notify()
		return
	}
	s.stopTimerLocked()
	s.active, s.paused, s.elapsed = false, false, 0
	rec, session, path := s.recording, s.session, s.output
	s.recording, s.session = nil, nil
	s.mu.Unlock()
	s.notify()
	go func() {
		rec.Stop()
		session.Close()
		s.mu.Lock()
		s.status = StatusIdle
		s.mu.Unlock()
		s.notify()
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			s.toast(i18n.T("recorder-toast-stopped"), toastIcon, stopToastMS)
			if s.hooks.Notify != nil {
				s.hooks.Notify(notifyAppName, i18n.T("recorder-notification-saved"), path, savedIcon)
			}
			return
		}
		log.Printf("recorder: recording produced no output file: %s", path)
		s.showError(i18n.T("recorder-toast-failed"))
	}()
}

// SetPaused pauses or resumes an active recording; an engine failure
// leaves the state as it was.
func (s *State) SetPaused(paused bool) {
	s.mu.Lock()
	rec := s.recording
	if !s.active || rec == nil || s.paused == paused {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if err := rec.SetPaused(paused); err != nil {
		log.Printf("recorder: set paused: %v", err)
		return
	}
	s.mu.Lock()
	if s.recording == rec {
		s.paused = paused
	}
	s.mu.Unlock()
	s.notify()
}

// resetIdle is reset_idle.
func (s *State) resetIdle() {
	s.mu.Lock()
	s.stopTimerLocked()
	s.status = StatusIdle
	s.preparing, s.active, s.paused = false, false, false
	s.elapsed = 0
	s.recording, s.session = nil, nil
	s.mu.Unlock()
	s.notify()
}

// startTimerLocked counts recorded seconds, skipping paused ones
// (start_timer). The caller holds mu.
func (s *State) startTimerLocked() {
	stop := make(chan struct{})
	s.timerStop = stop
	go func() {
		ticker := time.NewTicker(elapsedTick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				if s.timerStop != stop || !s.active {
					s.mu.Unlock()
					return
				}
				if s.paused {
					s.mu.Unlock()
					continue
				}
				s.elapsed++
				s.mu.Unlock()
				s.notify()
			}
		}
	}()
}

func (s *State) stopTimerLocked() {
	if s.timerStop != nil {
		close(s.timerStop)
		s.timerStop = nil
	}
}

func (s *State) toast(label, icon string, ms uint32) {
	if s.hooks.Toast != nil {
		s.hooks.Toast(label, icon, ms)
	}
}

// showError is show_error: a toast and a desktop notification.
func (s *State) showError(message string) {
	s.toast(message, errorIcon, stopToastMS)
	if s.hooks.Notify != nil {
		s.hooks.Notify(notifyAppName, i18n.T("recorder-notification-failed"), message, errorIcon)
	}
}

// BuildOptions is build_options: the recording's options from the
// config, the output named for now.
func BuildOptions(cfg config.RecorderConfig, now time.Time) Options {
	format := FormatMP4
	switch cfg.OutputFormat {
	case config.RecorderFormatMkv:
		format = FormatMKV
	case config.RecorderFormatWebm:
		format = FormatWebM
	}
	opts := Options{
		OutputPath:       OutputPath(cfg.OutputDirectory, format, now),
		Format:           format,
		Framerate:        cfg.Framerate,
		ShowCursor:       cfg.ShowCursor,
		Microphone:       cfg.Microphone,
		MicrophoneDevice: cfg.MicrophoneDevice,
		SystemAudio:      cfg.SystemAudio,
	}
	if cfg.WebcamEnabled {
		opts.Webcam = &Webcam{
			Device: cfg.WebcamDevice,
			X:      uint32(cfg.WebcamX),
			Y:      uint32(cfg.WebcamY),
			Size:   uint32(cfg.WebcamSize),
		}
	}
	return opts
}

// OutputPath is output_path: wayle-<local timestamp>.<ext> in the
// configured directory, else the videos directory.
func OutputPath(configuredDir string, format Format, now time.Time) string {
	dir := configuredDir
	if dir == "" {
		dir = VideosDir()
	}
	return filepath.Join(dir, "wayle-"+now.Format("20060102-150405")+"."+format.Extension())
}

// VideosDir is videos_dir: $XDG_VIDEOS_DIR, else $HOME/Videos.
func VideosDir() string {
	if dir := os.Getenv("XDG_VIDEOS_DIR"); dir != "" {
		return dir
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, "Videos")
	}
	return "."
}

// FormatElapsed is format_elapsed: H:MM:SS above the hour, M:SS within.
func FormatElapsed(totalSecs uint32) string {
	hours := totalSecs / 3600
	minutes := (totalSecs % 3600) / 60
	seconds := totalSecs % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
